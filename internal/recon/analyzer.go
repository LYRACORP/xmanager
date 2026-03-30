package recon

import (
	"context"
	"fmt"
	"time"

	"github.com/lyracorp/xmanager/internal/ai"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

const analysisPrompt = `Analyze the following server reconnaissance data and generate a structured JSON profile.

The output MUST be valid JSON with this exact structure:
{
  "hostname": "string",
  "os": "string (e.g. Ubuntu 22.04 LTS)",
  "kernel": "string",
  "cpu": "string (model + core count)",
  "ram_mb": number,
  "disk_gb": number,
  "services": [
    {
      "name": "string",
      "type": "string (webserver|database|runtime|cache|process_manager|other)",
      "tech": "string (e.g. nginx 1.24, PostgreSQL 16)",
      "status": "running|stopped|unknown",
      "port": "string or empty",
      "tags": ["string"]
    }
  ],
  "domains": ["string"],
  "ssl_certs": [
    {
      "domain": "string",
      "expires": "string (date)"
    }
  ],
  "open_ports": ["string"],
  "firewall": "string (active|inactive|unknown)",
  "deployment_method": "string (docker|pm2|systemd|manual|mixed)",
  "summary": "string (2-3 sentence human-readable summary)"
}

Only output the JSON. No markdown, no explanation.

=== SERVER RECON DATA ===
%s`

func Analyze(ctx context.Context, provider ai.Provider, scanResult *ScanResult) (string, error) {
	prompt := fmt.Sprintf(analysisPrompt, scanResult.Raw)

	messages := []ai.Message{
		{Role: ai.RoleUser, Content: prompt},
	}

	result, err := provider.Chat(ctx, messages, ai.WithTemperature(0.1), ai.WithMaxTokens(4096))
	if err != nil {
		return "", fmt.Errorf("AI analysis: %w", err)
	}

	return result, nil
}

func SaveProfile(db *gorm.DB, serverID uint, profileJSON string) error {
	profile := storage.ServerProfile{
		ServerID:    serverID,
		ProfileJSON: profileJSON,
		ScannedAt:   time.Now(),
	}
	return db.Create(&profile).Error
}

func GetLatestProfile(db *gorm.DB, serverID uint) (*storage.ServerProfile, error) {
	var profile storage.ServerProfile
	err := db.Where("server_id = ?", serverID).Order("scanned_at DESC").First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}
