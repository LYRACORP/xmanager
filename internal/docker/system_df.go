package docker

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// SystemDFRow is one line from `docker system df`.
type SystemDFRow struct {
	Type        string // Images, Containers, Local Volumes, Build Cache
	TotalCount  int
	Active      int
	SizeBytes   uint64
	SizeHuman   string
	Reclaimable uint64
}

// SystemDF runs `docker system df --format '{{json .}}'` and parses rows.
func (m *Manager) SystemDF() ([]SystemDFRow, error) {
	result, err := m.exec.Run("docker system df --format '{{json .}}' 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if result == nil || strings.TrimSpace(result.Stdout) == "" {
		return nil, nil
	}
	return ParseSystemDF(result.Stdout)
}

// ParseSystemDF parses newline-delimited JSON from docker system df.
func ParseSystemDF(stdout string) ([]SystemDFRow, error) {
	var rows []SystemDFRow
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			Type        string `json:"Type"`
			TotalCount  string `json:"TotalCount"`
			Active      string `json:"Active"`
			Size        string `json:"Size"`
			Reclaimable string `json:"Reclaimable"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw.Type == "" {
			continue
		}
		sizeStr := raw.Size
		// Reclaimable often looks like "1.2GB (50%)" — take leading size only for Size field.
		reclaimStr := raw.Reclaimable
		if i := strings.Index(reclaimStr, " "); i > 0 {
			reclaimStr = reclaimStr[:i]
		}
		row := SystemDFRow{
			Type:        raw.Type,
			TotalCount:  atoiDef(raw.TotalCount),
			Active:      atoiDef(raw.Active),
			SizeHuman:   sizeStr,
			SizeBytes:   ParseDockerSize(sizeStr),
			Reclaimable: ParseDockerSize(reclaimStr),
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no docker system df rows")
	}
	return rows, nil
}

func atoiDef(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// ParseDockerSize converts docker size strings like "1.5GB", "512MB", "1024B" to bytes.
func ParseDockerSize(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0
	}
	// Strip trailing "(xx%)" if present.
	if i := strings.Index(s, "("); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	var numStr strings.Builder
	var unitStr strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) || r == '.' {
			numStr.WriteRune(r)
		} else if unicode.IsLetter(r) {
			unitStr.WriteRune(r)
		}
	}
	f, err := strconv.ParseFloat(numStr.String(), 64)
	if err != nil || f < 0 {
		return 0
	}
	unit := strings.ToUpper(unitStr.String())
	mult := float64(1)
	switch unit {
	case "B", "":
		mult = 1
	case "KB", "KIB", "K":
		mult = 1024
	case "MB", "MIB", "M":
		mult = 1024 * 1024
	case "GB", "GIB", "G":
		mult = 1024 * 1024 * 1024
	case "TB", "TIB", "T":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return uint64(f * mult)
}

// DFBytesByType returns SizeBytes for a Type substring match (case-insensitive).
func DFBytesByType(rows []SystemDFRow, typeName string) uint64 {
	want := strings.ToLower(typeName)
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Type), want) {
			return r.SizeBytes
		}
	}
	return 0
}

// DFRowByType returns the first SystemDFRow whose Type contains typeName.
func DFRowByType(rows []SystemDFRow, typeName string) (SystemDFRow, bool) {
	want := strings.ToLower(typeName)
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.Type), want) {
			return r, true
		}
	}
	return SystemDFRow{}, false
}

// FormatSize formats bytes using docker-style units (1.5GB, 512MB).
func FormatSize(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}
