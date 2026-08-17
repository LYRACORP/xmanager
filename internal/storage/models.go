package storage

import (
	"time"

	"gorm.io/gorm"
)

type Server struct {
	gorm.Model
	Name       string     `gorm:"not null" json:"name"`
	Host       string     `gorm:"not null" json:"host"`
	Port       int        `gorm:"default:22" json:"port"`
	User       string     `gorm:"not null" json:"user"`
	SSHKeyPath string     `json:"ssh_key_path"`
	Password   string     `json:"password,omitempty"`
	Tags       string     `json:"tags"`
	JumpHost   string     `json:"jump_host"`
	IsActive   bool       `gorm:"default:false" json:"is_active"`
	LastSeen   *time.Time `json:"last_seen"`
}

type ServerProfile struct {
	gorm.Model
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Server      Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	ProfileJSON string    `gorm:"type:text" json:"profile_json"`
	ScannedAt   time.Time `json:"scanned_at"`
}

type ServerMetricSnapshot struct {
	gorm.Model
	ServerID       uint      `gorm:"index;not null" json:"server_id"`
	Server         Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	SampledAt      time.Time `gorm:"index" json:"sampled_at"`
	CPUPct         float64   `json:"cpu_pct"`
	RAMPct         float64   `json:"ram_pct"`
	RAMUsedMB      float64   `json:"ram_used_mb"`
	RAMTotalMB     float64   `json:"ram_total_mb"`
	DiskPct        float64   `json:"disk_pct"`
	DiskUsedGB     float64   `json:"disk_used_gb"`
	DiskTotalGB    float64   `json:"disk_total_gb"`
	NetRxKBps      float64   `json:"net_rx_kbps"`
	NetTxKBps      float64   `json:"net_tx_kbps"`
	ContainerCount int       `json:"container_count"`
	UptimeStatus   string    `gorm:"default:unknown" json:"uptime_status"` // up, down, unknown
	Online         bool      `json:"online"`
}

// MetricSample is a compact host or DB engine point for web panel charts.
// Kind is "host" or "db:<engine>". Net fields are bytes/sec (not cumulative).
type MetricSample struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ServerID  uint      `gorm:"uniqueIndex:idx_metric_sample_skt;not null" json:"server_id"`
	Kind      string    `gorm:"uniqueIndex:idx_metric_sample_skt;size:32;not null" json:"kind"`
	SampledAt time.Time `gorm:"uniqueIndex:idx_metric_sample_skt;index;not null" json:"sampled_at"`
	CPUPct    float64   `json:"cpu_pct"`
	RAMPct    float64   `json:"ram_pct"`
	DiskPct   float64   `json:"disk_pct"`
	NetRxBps  float64   `json:"net_rx_bps"`
	NetTxBps  float64   `json:"net_tx_bps"`
	MemMB     float64   `json:"mem_mb"`
}

type ErrorEvent struct {
	gorm.Model
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Server      Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Service     string    `json:"service"`
	Fingerprint string    `gorm:"index" json:"fingerprint"`
	Message     string    `gorm:"type:text" json:"message"`
	StackTrace  string    `gorm:"type:text" json:"stack_trace"`
	Severity    string    `gorm:"default:error" json:"severity"`
	Count       int       `gorm:"default:1" json:"count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Resolved    bool      `gorm:"default:false" json:"resolved"`
	Muted       bool      `gorm:"default:false" json:"muted"`
}

type AlertRule struct {
	gorm.Model
	ServerID  uint   `gorm:"index" json:"server_id"`
	MonitorID uint   `gorm:"index" json:"monitor_id"`
	Type      string `gorm:"not null" json:"type"` // cpu, ram, disk, error, unreachable, uptime
	Threshold string `json:"threshold"`
	ChannelID uint   `json:"channel_id"`
	Channel   string `gorm:"default:telegram" json:"channel"`
	Enabled   bool   `gorm:"default:true" json:"enabled"`
}

type DeployHistory struct {
	gorm.Model
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Server      Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	ProjectID   uint      `gorm:"index" json:"project_id"`
	Service     string    `json:"service"`
	Method      string    `json:"method"`
	Status      string    `json:"status"`
	Output      string    `gorm:"type:text" json:"output"`
	TriggeredAt time.Time `json:"triggered_at"`
}

type AISession struct {
	gorm.Model
	ServerID     uint   `gorm:"index" json:"server_id"`
	Server       Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Title        string `json:"title"`
	MessagesJSON string `gorm:"type:text" json:"messages_json"`
}

type Backup struct {
	gorm.Model
	ServerID     uint      `gorm:"index;not null" json:"server_id"`
	Server       Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Type         string    `json:"type"` // backup_database, backup_directory, cut_log, full_backup, sync_time, free_ram, access_url, shell; also legacy postgres/mysql/...
	Service      string    `json:"service"`
	Path         string    `json:"path"`
	Filename     string    `json:"filename"`
	Size         int64     `json:"size"`
	Schedule     string    `json:"schedule"`
	Status       string    `json:"status"`
	Destinations string    `json:"destinations"` // comma-separated: local,ftp:1,scp:2,telegram:3
	Error        string    `gorm:"type:text" json:"error"`
	TaskConfig   string    `gorm:"type:text" json:"task_config"` // JSON extras per task type
	BackedAt     time.Time `json:"backed_at"`
}

// BackupDestination is a saved FTP/SCP/Telegram delivery target.
type BackupDestination struct {
	gorm.Model
	ServerID   uint   `gorm:"index;not null" json:"server_id"`
	Server     Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Name       string `gorm:"not null" json:"name"`
	Type       string `gorm:"not null" json:"type"` // ftp, scp, telegram
	ConfigJSON string `gorm:"type:text" json:"config_json"`
	Enabled    bool   `gorm:"default:true" json:"enabled"`
}

// ActivityLog records panel and TUI mutations for the Logs UI.
type ActivityLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	ServerID  uint      `gorm:"index" json:"server_id"`
	Source    string    `gorm:"index;size:16" json:"source"` // web | tui
	Actor     string    `gorm:"size:128" json:"actor"`
	Action    string    `gorm:"size:64" json:"action"`
	Method    string    `gorm:"size:16" json:"method"`
	Path      string    `gorm:"size:512" json:"path"`
	Resource  string    `gorm:"size:256" json:"resource"`
	Detail    string    `gorm:"type:text" json:"detail"`
	Status    string    `gorm:"size:16" json:"status"` // ok | error
	IP        string    `gorm:"size:64" json:"ip"`
}

// SecurityEvent records login attempts, SSH failures, fail2ban, and scan tags.
type SecurityEvent struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	ServerID  uint      `gorm:"index" json:"server_id"`
	Kind      string    `gorm:"index;size:32" json:"kind"` // login_ok | login_fail | ssh_fail | fail2ban | scan
	IP        string    `gorm:"index;size:64" json:"ip"`
	Actor     string    `gorm:"size:128" json:"actor"`
	Detail    string    `gorm:"type:text" json:"detail"`
}

// RequestDump stores captured inbound HTTP/TCP payloads for forensic review.
type RequestDump struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
	ServerID   uint      `gorm:"index" json:"server_id"`
	Source     string    `gorm:"index;size:16" json:"source"` // panel | nginx | honeypot
	ListenPort int       `json:"listen_port"`
	RemoteIP   string    `gorm:"index;size:64" json:"remote_ip"`
	Method     string    `gorm:"size:16" json:"method"`
	Path       string    `gorm:"size:512" json:"path"`
	Query      string    `gorm:"size:512" json:"query"`
	Proto      string    `gorm:"size:16" json:"proto"`
	Headers    string    `gorm:"type:text" json:"headers"`
	Body       []byte    `gorm:"type:blob" json:"body"`
	Truncated  bool      `json:"truncated"`
	Status     int       `json:"status"`
}

type AIConfigRecord struct {
	gorm.Model
	Provider        string `json:"provider"`
	LLMModel        string `json:"model"`
	APIKeyEncrypted string `json:"api_key_encrypted"`
	Endpoint        string `json:"endpoint"`
	IsDefault       bool   `gorm:"default:false" json:"is_default"`
}

func (AIConfigRecord) TableName() string {
	return "ai_configs"
}

type User struct {
	gorm.Model
	Username     string `gorm:"uniqueIndex;not null" json:"username"`
	PasswordHash string `gorm:"not null" json:"-"`
	Role         string `gorm:"default:admin" json:"role"` // admin, operator, viewer
}

type Project struct {
	gorm.Model
	Name           string `gorm:"not null" json:"name"`
	ServerID       uint   `gorm:"index;not null" json:"server_id"`
	Server         Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Type           string `gorm:"not null" json:"type"` // image, compose, dockerfile, git, oneclick, clone, push, scratch, archive, function
	Source         string `gorm:"type:text" json:"source"`
	Domain         string `json:"domain"`
	PersistentData bool   `gorm:"default:false" json:"persistent_data"`
	DeployStatus   string `gorm:"default:pending" json:"deploy_status"`
	ConfigJSON     string `gorm:"type:text" json:"config_json"`
	WebhookSecret  string `json:"webhook_secret"`
}

type ProjectDomain struct {
	gorm.Model
	ProjectID uint    `gorm:"index;not null" json:"project_id"`
	Project   Project `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Domain    string  `gorm:"not null" json:"domain"`
	SSL       bool    `gorm:"default:true" json:"ssl"`
}

type ProjectEnvVar struct {
	gorm.Model
	ProjectID      uint    `gorm:"index;not null" json:"project_id"`
	Project        Project `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Key            string  `gorm:"not null" json:"key"`
	ValueEncrypted string  `gorm:"type:text" json:"-"`
}

type GitCredential struct {
	gorm.Model
	Provider         string     `gorm:"not null;index" json:"provider"` // github, gitlab, bitbucket, gitea
	Username         string     `json:"username"`
	AccountLogin     string     `json:"account_login"`
	TokenEncrypted   string     `gorm:"type:text" json:"-"`
	RefreshEncrypted string     `gorm:"type:text" json:"-"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Endpoint         string     `json:"endpoint"`
	Scopes           string     `json:"scopes"`
}

// GitOAuthApp holds per-provider OAuth application credentials (client id/secret).
type GitOAuthApp struct {
	gorm.Model
	Provider              string `gorm:"uniqueIndex;not null" json:"provider"` // github, gitlab, bitbucket, gitea
	ClientID              string `json:"client_id"`
	ClientSecretEncrypted string `gorm:"type:text" json:"-"`
	Endpoint              string `json:"endpoint"` // self-hosted GitLab/Gitea base URL
	Enabled               bool   `gorm:"default:true" json:"enabled"`
}

type CronJob struct {
	gorm.Model
	ServerID   uint       `gorm:"index;not null" json:"server_id"`
	Server     Server     `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Name       string     `gorm:"not null" json:"name"`
	Expression string     `gorm:"not null" json:"expression"`
	Command    string     `gorm:"type:text;not null" json:"command"`
	TaskType   string     `json:"task_type"`                    // backup_database, shell, …; empty = legacy raw command
	TaskConfig string     `gorm:"type:text" json:"task_config"` // JSON extras per task type
	Enabled    bool       `gorm:"default:true" json:"enabled"`
	LastRun    *time.Time `json:"last_run"`
	NextRun    *time.Time `json:"next_run"`
	Status     string     `gorm:"default:idle" json:"status"`
}

type CronRun struct {
	gorm.Model
	CronJobID uint      `gorm:"index;not null" json:"cron_job_id"`
	CronJob   CronJob   `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	StartedAt time.Time `json:"started_at"`
	ExitCode  int       `json:"exit_code"`
	Log       string    `gorm:"type:text" json:"log"`
}

type UptimeMonitor struct {
	gorm.Model
	Name          string `gorm:"not null" json:"name"`
	ServerID      uint   `gorm:"index" json:"server_id"`
	URL           string `json:"url"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	IntervalSec   int    `gorm:"default:60" json:"interval_sec"`
	AlertChannels string `json:"alert_channels"`
	Enabled       bool   `gorm:"default:true" json:"enabled"`
	LastStatus    string `gorm:"default:unknown" json:"last_status"`
}

type UptimeEvent struct {
	gorm.Model
	MonitorID uint          `gorm:"index;not null" json:"monitor_id"`
	Monitor   UptimeMonitor `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Status    string        `json:"status"` // up, down, degraded
	CheckedAt time.Time     `json:"checked_at"`
	LatencyMS int64         `json:"latency_ms"`
	Message   string        `json:"message"`
}

type AlertChannel struct {
	gorm.Model
	Name       string `gorm:"not null" json:"name"`
	Type       string `gorm:"not null" json:"type"` // telegram, email, sms, webhook
	ConfigJSON string `gorm:"type:text" json:"config_json"`
	Enabled    bool   `gorm:"default:true" json:"enabled"`
}

type ServiceInstance struct {
	gorm.Model
	ServerID    uint   `gorm:"index;not null" json:"server_id"`
	Server      Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	ServiceType string `gorm:"not null;index" json:"service_type"` // registry, gitea, rustfs, rabbitmq, kafka, mattermost, bugsink, netdata, umami, powerdns, mailinbox, uptimekuma, databasus, k8s
	Enabled     bool   `gorm:"default:false" json:"enabled"`
	ConfigJSON  string `gorm:"type:text" json:"config_json"`
	Status      string `gorm:"default:stopped" json:"status"`
}

type ScriptRun struct {
	gorm.Model
	Name       string    `gorm:"not null" json:"name"`
	ScriptType string    `json:"script_type"` // bash, python, node
	Content    string    `gorm:"type:text" json:"content"`
	TargetIDs  string    `json:"target_ids"` // comma-separated server IDs or "all"
	StartedAt  time.Time `json:"started_at"`
	ExitCode   int       `json:"exit_code"`
	Output     string    `gorm:"type:text" json:"output"`
}

type DatabaseUser struct {
	gorm.Model
	ServerID  uint   `gorm:"index;not null" json:"server_id"`
	Server    Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	DBType    string `gorm:"not null" json:"db_type"`
	Username  string `gorm:"not null" json:"username"`
	Databases string `json:"databases"` // comma-separated
	Host      string `gorm:"default:%" json:"host"`
}

// ConnectedDomain is a first-class domain hub on a node (DNS + mail + project + DB links).
type ConnectedDomain struct {
	gorm.Model
	ServerID    uint   `gorm:"uniqueIndex:uidx_connected_domain;not null" json:"server_id"`
	Server      Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Domain      string `gorm:"uniqueIndex:uidx_connected_domain;not null" json:"domain"`
	ProjectID   *uint  `gorm:"index" json:"project_id,omitempty"`
	Upstream    string `json:"upstream"`
	PublicIP    string `json:"public_ip"`
	DBType      string `json:"db_type"`
	DBName      string `json:"db_name"`
	DNSReady    bool   `gorm:"default:false" json:"dns_ready"`
	MailReady   bool   `gorm:"default:false" json:"mail_ready"`
	LastError   string `gorm:"type:text" json:"last_error"`
	DNSProvider string `json:"dns_provider"` // "powerdns" | "cloudflare" | ""
	CFZoneID    string `json:"cf_zone_id"`   // Cloudflare Zone ID when provider=cloudflare
}

// NodeSettings holds node-level identity used for DNS and Cloudflare integration.
type NodeSettings struct {
	gorm.Model
	ServerID   uint   `gorm:"uniqueIndex;not null" json:"server_id"`
	Server     Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	MainDomain string `json:"main_domain"` // e.g. panel.example.com
	NS1        string `json:"ns1"`         // e.g. ns1.example.com
	NS2        string `json:"ns2"`         // e.g. ns2.example.com
	PublicIP   string `json:"public_ip"`
	CFAPIToken string `json:"cf_api_token"` // global Cloudflare API token
}

// Mailbox is an email address on a connected domain (Stalwart / Mail-in-a-Box API).
type Mailbox struct {
	gorm.Model
	ServerID  uint   `gorm:"index;not null" json:"server_id"`
	Server    Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Domain    string `gorm:"not null;index" json:"domain"`
	Address   string `gorm:"not null" json:"address"` // local-part@domain
	ProjectID *uint  `gorm:"index" json:"project_id,omitempty"`
}

// ProjectDatabase links a project to a database name/engine.
type ProjectDatabase struct {
	gorm.Model
	ProjectID uint    `gorm:"index;not null" json:"project_id"`
	Project   Project `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	ServerID  uint    `gorm:"index;not null" json:"server_id"`
	DBType    string  `gorm:"not null" json:"db_type"`
	DBName    string  `gorm:"not null" json:"db_name"`
	Username  string  `json:"username"`
}

// FTPUser is a jailed vsftpd account on a managed server (no password stored).
type FTPUser struct {
	gorm.Model
	ServerID uint   `gorm:"uniqueIndex:uidx_ftp_user;not null" json:"server_id"`
	Server   Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Username string `gorm:"uniqueIndex:uidx_ftp_user;not null" json:"username"`
	Home     string `gorm:"not null" json:"home"`
	Enabled  bool   `gorm:"default:true" json:"enabled"`
	Notes    string `json:"notes"`
}
