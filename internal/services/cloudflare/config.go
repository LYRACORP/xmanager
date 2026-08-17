package cloudflare

import (
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// Config holds Cloudflare API credentials for a zone.
type Config struct {
	APIToken string
	ZoneID   string
}

// LoadToken returns the global Cloudflare API token from NodeSettings.
func LoadToken(db *gorm.DB, serverID uint) string {
	if db == nil || serverID == 0 {
		return ""
	}
	var ns storage.NodeSettings
	if err := db.Where("server_id = ?", serverID).First(&ns).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(ns.CFAPIToken)
}

// LoadNodeSettings returns NodeSettings for a server (empty struct if missing).
func LoadNodeSettings(db *gorm.DB, serverID uint) storage.NodeSettings {
	var ns storage.NodeSettings
	if db == nil || serverID == 0 {
		return ns
	}
	_ = db.Where("server_id = ?", serverID).First(&ns)
	return ns
}

// NewConfig builds a Config from token + zone ID.
func NewConfig(token, zoneID string) Config {
	return Config{
		APIToken: strings.TrimSpace(token),
		ZoneID:   strings.TrimSpace(zoneID),
	}
}
