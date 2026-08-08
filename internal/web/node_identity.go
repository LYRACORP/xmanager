package web

import (
	"errors"
	"fmt"
	"os"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const localServerName = "local-node"

// EnsureLocalServer creates or returns the sentinel Server row used by the node
// panel for FK-backed models (projects, cron, services, mailboxes).
func EnsureLocalServer(db *gorm.DB) (storage.Server, error) {
	var srv storage.Server
	err := db.Where("name = ?", localServerName).First(&srv).Error
	if err == nil {
		return srv, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return srv, err
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	srv = storage.Server{
		Name: localServerName,
		Host: host,
		Port: 22,
		User: "root",
		Tags: "node,local",
	}
	if err := db.Where("name = ?", localServerName).FirstOrCreate(&srv, srv).Error; err != nil {
		return srv, fmt.Errorf("ensure local server: %w", err)
	}
	return srv, nil
}
