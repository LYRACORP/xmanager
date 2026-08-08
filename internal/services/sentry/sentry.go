// Package sentry is a deprecated alias for Bugsink (Sentry-compatible error tracking).
// Prefer github.com/lyracorp/xmanager/internal/services/bugsink.
package sentry

import (
	"github.com/lyracorp/xmanager/internal/services/bugsink"
	"gorm.io/gorm"
)

// New returns a Bugsink deployer (replaces the old GlitchTip-based Sentry stack).
func New(db *gorm.DB, serverID uint) *bugsink.Bugsink {
	return bugsink.New(db, serverID)
}
