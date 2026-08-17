package config

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const (
	AccessKeyLen    = 16
	AccessKeyMinLen = 8
	AccessKeyMaxLen = 64
)

const accessKeyAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

var reservedAccessKeys = map[string]bool{
	"static": true, "login": true, "logout": true, "setup": true,
	"api": true, "settings": true, "projects": true, "oauth": true,
	"webhook": true, "internal": true, "docker": true, "cron": true,
	"databases": true, "backup": true, "storage": true, "ftp": true,
	"domains": true, "email": true, "apps": true, "services": true,
	"security": true, "logs": true, "alerts": true, "netdata": true,
	"oneclick": true, "dns": true, "uptime": true, "servers": true,
}

// GenerateAccessKey returns a 16-character random a-z0-9 key.
func GenerateAccessKey() (string, error) {
	b := make([]byte, AccessKeyLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating access key: %w", err)
	}
	out := make([]byte, AccessKeyLen)
	n := byte(len(accessKeyAlphabet))
	for i, v := range b {
		out[i] = accessKeyAlphabet[v%n]
	}
	return string(out), nil
}

// NormalizeAccessKey trims slashes, lowercases, and validates charset/length.
func NormalizeAccessKey(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "/")
	if s == "" {
		return "", fmt.Errorf("access key required")
	}
	if len(s) < AccessKeyMinLen || len(s) > AccessKeyMaxLen {
		return "", fmt.Errorf("access key must be %d–%d characters", AccessKeyMinLen, AccessKeyMaxLen)
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return "", fmt.Errorf("access key may only contain a-z and 0-9")
		}
	}
	if reservedAccessKeys[s] {
		return "", fmt.Errorf("access key %q is reserved", s)
	}
	return s, nil
}

// EnsureAccessKey generates and persists a key when none is set (or the stored value is invalid).
func EnsureAccessKey(cfg *Config) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config required")
	}
	if key, err := NormalizeAccessKey(cfg.Web.AccessKey); err == nil {
		cfg.Web.AccessKey = key
		return key, nil
	}
	key, err := GenerateAccessKey()
	if err != nil {
		return "", err
	}
	cfg.Web.AccessKey = key
	if err := Save(cfg); err != nil {
		return "", err
	}
	return key, nil
}
