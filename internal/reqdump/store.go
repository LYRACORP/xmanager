package reqdump

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const (
	SourcePanel    = "panel"
	SourceNginx    = "nginx"
	SourceHoneypot = "honeypot"

	ServiceType = "reqdump"

	maxDumpRows          = 10000
	dumpRetentionDays    = 14
	maxBodyBytes         = 64 * 1024
	defaultHoneypotPorts = "2323,8081,8888"
)

// Config toggles for request capture.
type Config struct {
	Enabled       bool
	DumpPanel     bool
	DumpNginx     bool
	DumpHoneypot  bool
	HoneypotPorts string
	SinkPort      int // loopback nginx mirror sink
}

// DefaultConfig returns off-by-default settings.
func DefaultConfig() Config {
	return Config{
		HoneypotPorts: defaultHoneypotPorts,
		SinkPort:      18089,
	}
}

// LoadConfig reads reqdump ServiceInstance for a server.
func LoadConfig(db *gorm.DB, serverID uint) Config {
	cfg := DefaultConfig()
	if db == nil || serverID == 0 {
		return cfg
	}
	var inst storage.ServiceInstance
	if err := db.Where("server_id = ? AND service_type = ?", serverID, ServiceType).First(&inst).Error; err != nil {
		return cfg
	}
	cfg.Enabled = inst.Enabled
	if inst.ConfigJSON == "" || inst.ConfigJSON == "{}" {
		return cfg
	}
	var raw map[string]interface{}
	if json.Unmarshal([]byte(inst.ConfigJSON), &raw) != nil {
		return cfg
	}
	cfg.DumpPanel = boolVal(raw, "dump_panel")
	cfg.DumpNginx = boolVal(raw, "dump_nginx")
	cfg.DumpHoneypot = boolVal(raw, "dump_honeypot")
	if s, ok := raw["honeypot_ports"].(string); ok && strings.TrimSpace(s) != "" {
		cfg.HoneypotPorts = strings.TrimSpace(s)
	}
	if n, ok := raw["sink_port"].(float64); ok && n > 0 {
		cfg.SinkPort = int(n)
	}
	return cfg
}

// SaveConfig persists reqdump ServiceInstance.
func SaveConfig(db *gorm.DB, serverID uint, cfg Config) error {
	if db == nil || serverID == 0 {
		return nil
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"dump_panel":     cfg.DumpPanel,
		"dump_nginx":     cfg.DumpNginx,
		"dump_honeypot":  cfg.DumpHoneypot,
		"honeypot_ports": cfg.HoneypotPorts,
		"sink_port":      cfg.SinkPort,
	})
	status := "stopped"
	if cfg.Enabled {
		status = "running"
	}
	var inst storage.ServiceInstance
	res := db.Where("server_id = ? AND service_type = ?", serverID, ServiceType).First(&inst)
	if res.Error != nil {
		inst = storage.ServiceInstance{
			ServerID:    serverID,
			ServiceType: ServiceType,
			Enabled:     cfg.Enabled,
			Status:      status,
			ConfigJSON:  string(raw),
		}
		return db.Create(&inst).Error
	}
	inst.Enabled = cfg.Enabled
	inst.Status = status
	inst.ConfigJSON = string(raw)
	return db.Save(&inst).Error
}

func boolVal(m map[string]interface{}, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "on" || t == "1"
	default:
		return false
	}
}

// Capture is one recorded request.
type Capture struct {
	ServerID   uint
	Source     string
	ListenPort int
	RemoteIP   string
	Method     string
	Path       string
	Query      string
	Proto      string
	Headers    string
	Body       []byte
	Truncated  bool
	Status     int
}

// Record stores a dump row.
func Record(db *gorm.DB, c Capture) (uint, error) {
	if db == nil {
		return 0, nil
	}
	body := c.Body
	truncated := c.Truncated
	if len(body) > maxBodyBytes {
		body = body[:maxBodyBytes]
		truncated = true
	}
	row := storage.RequestDump{
		CreatedAt:  time.Now(),
		ServerID:   c.ServerID,
		Source:     truncate(c.Source, 16),
		ListenPort: c.ListenPort,
		RemoteIP:   truncate(c.RemoteIP, 64),
		Method:     truncate(c.Method, 16),
		Path:       truncate(c.Path, 512),
		Query:      truncate(c.Query, 512),
		Proto:      truncate(c.Proto, 16),
		Headers:    truncate(c.Headers, 16000),
		Body:       body,
		Truncated:  truncated,
		Status:     c.Status,
	}
	if err := db.Create(&row).Error; err != nil {
		return 0, err
	}
	trim(db)
	return row.ID, nil
}

// Get returns one dump by id.
func Get(db *gorm.DB, serverID, id uint) (*storage.RequestDump, error) {
	if db == nil {
		return nil, nil
	}
	var row storage.RequestDump
	err := db.Where("id = ? AND server_id = ?", id, serverID).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// ListFilter for dump queries.
type ListFilter struct {
	ServerID uint
	Source   string
	Limit    int
}

// List returns newest dumps.
func List(db *gorm.DB, f ListFilter) ([]storage.RequestDump, error) {
	if db == nil {
		return nil, nil
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := db.Model(&storage.RequestDump{}).Order("id desc")
	if f.ServerID > 0 {
		q = q.Where("server_id = ?", f.ServerID)
	}
	if f.Source != "" {
		q = q.Where("source = ?", f.Source)
	}
	var rows []storage.RequestDump
	err := q.Limit(limit).Find(&rows).Error
	return rows, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func trim(db *gorm.DB) {
	cutoff := time.Now().AddDate(0, 0, -dumpRetentionDays)
	_ = db.Where("created_at < ?", cutoff).Delete(&storage.RequestDump{}).Error
	var count int64
	if err := db.Model(&storage.RequestDump{}).Count(&count).Error; err != nil || count <= maxDumpRows {
		return
	}
	var row storage.RequestDump
	if err := db.Order("id desc").Offset(maxDumpRows).Limit(1).Find(&row).Error; err != nil || row.ID == 0 {
		return
	}
	_ = db.Where("id <= ?", row.ID).Delete(&storage.RequestDump{}).Error
}

// RedactHeaders masks sensitive headers for panel captures.
func RedactHeaders(headers string) string {
	lines := strings.Split(headers, "\n")
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "authorization:") ||
			strings.HasPrefix(lower, "cookie:") ||
			strings.HasPrefix(lower, "set-cookie:") {
			if idx := strings.Index(line, ":"); idx >= 0 {
				lines[i] = line[:idx+1] + " [redacted]"
			}
		}
	}
	return strings.Join(lines, "\n")
}

// RedactBody masks password fields in form bodies for panel captures.
func RedactBody(body []byte, contentType string) []byte {
	if len(body) == 0 {
		return body
	}
	if strings.Contains(strings.ToLower(contentType), "application/x-www-form-urlencoded") {
		s := string(body)
		for _, key := range []string{"password", "passwd", "token", "secret"} {
			s = redactFormField(s, key)
		}
		return []byte(s)
	}
	return body
}

func redactFormField(s, key string) string {
	parts := strings.Split(s, "&")
	for i, p := range parts {
		if strings.HasPrefix(strings.ToLower(p), key+"=") {
			parts[i] = key + "=[redacted]"
		}
	}
	return strings.Join(parts, "&")
}

// ParseHoneypotPorts splits comma-separated port list.
func ParseHoneypotPorts(spec string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var p int
		p, err := strconv.Atoi(part)
		if err != nil || p <= 0 || p > 65535 {
			continue
		}
		if !seen[p] {
			seen[p] = true
			ports = append(ports, p)
		}
	}
	return ports
}
