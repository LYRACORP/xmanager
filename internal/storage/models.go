package storage

import (
	"time"

	"gorm.io/gorm"
)

type Server struct {
	gorm.Model
	Name       string `gorm:"not null" json:"name"`
	Host       string `gorm:"not null" json:"host"`
	Port       int    `gorm:"default:22" json:"port"`
	User       string `gorm:"not null" json:"user"`
	SSHKeyPath string `json:"ssh_key_path"`
	Password   string `json:"password,omitempty"`
	Tags       string `json:"tags"`
	JumpHost   string `json:"jump_host"`
	IsActive   bool   `gorm:"default:false" json:"is_active"`
	LastSeen   *time.Time `json:"last_seen"`
}

type ServerProfile struct {
	gorm.Model
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Server      Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	ProfileJSON string    `gorm:"type:text" json:"profile_json"`
	ScannedAt   time.Time `json:"scanned_at"`
}

type ErrorEvent struct {
	gorm.Model
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Server      Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Service     string    `json:"service"`
	Fingerprint string    `gorm:"index" json:"fingerprint"`
	Message     string    `gorm:"type:text" json:"message"`
	StackTrace  string    `gorm:"type:text" json:"stack_trace"`
	Severity    string    `gorm:"default:error" json:"severity"` // info, warning, error, critical
	Count       int       `gorm:"default:1" json:"count"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Resolved    bool      `gorm:"default:false" json:"resolved"`
	Muted       bool      `gorm:"default:false" json:"muted"`
}

type AlertRule struct {
	gorm.Model
	ServerID  uint   `gorm:"index" json:"server_id"`
	Server    Server `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Type      string `gorm:"not null" json:"type"` // cpu, ram, disk, error, unreachable
	Threshold string `json:"threshold"`
	Channel   string `gorm:"default:telegram" json:"channel"`
	Enabled   bool   `gorm:"default:true" json:"enabled"`
}

type DeployHistory struct {
	gorm.Model
	ServerID    uint      `gorm:"index;not null" json:"server_id"`
	Server      Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Service     string    `json:"service"`
	Method      string    `json:"method"` // docker, git, manual
	Status      string    `json:"status"` // success, failed, in_progress
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
	ServerID  uint      `gorm:"index;not null" json:"server_id"`
	Server    Server    `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
	Type      string    `json:"type"` // postgres, mysql, mongodb, volume
	Service   string    `json:"service"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	Schedule  string    `json:"schedule"`
	Status    string    `json:"status"` // success, failed, in_progress
	BackedAt  time.Time `json:"backed_at"`
}

type AIConfigRecord struct {
	gorm.Model
	Provider        string `json:"provider"`
	LLMModel        string `json:"model"`
	APIKeyEncrypted string `json:"api_key_encrypted"`
	Endpoint       string `json:"endpoint"`
	IsDefault      bool   `gorm:"default:false" json:"is_default"`
}

func (AIConfigRecord) TableName() string {
	return "ai_configs"
}
