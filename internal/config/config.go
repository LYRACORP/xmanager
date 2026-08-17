package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

type Config struct {
	DataDir    string       `mapstructure:"-"`
	ConfigPath string       `mapstructure:"-"`
	AI         AIConfig     `mapstructure:"ai"`
	Telegram   TGConfig     `mapstructure:"telegram"`
	UI         UIConfig     `mapstructure:"ui"`
	Log        LogConfig    `mapstructure:"log"`
	Web        WebConfig    `mapstructure:"web"`
	Poller     PollerConfig `mapstructure:"poller"`
	Email      EmailConfig  `mapstructure:"email"`
}

type AIConfig struct {
	Provider    string `mapstructure:"provider"`
	Model       string `mapstructure:"model"`
	APIKey      string `mapstructure:"api_key"`
	Endpoint    string `mapstructure:"endpoint"`
	OllamaHost  string `mapstructure:"ollama_host"`
	WhisperKey  string `mapstructure:"whisper_key"`
	MaxLogLines int    `mapstructure:"max_log_lines"`
}

type TGConfig struct {
	BotToken string `mapstructure:"bot_token"`
	ChatID   string `mapstructure:"chat_id"`
	Enabled  bool   `mapstructure:"enabled"`
}

type UIConfig struct {
	Theme       string `mapstructure:"theme"`
	RefreshRate int    `mapstructure:"refresh_rate"`
}

type LogConfig struct {
	Level         string `mapstructure:"level"`
	File          string `mapstructure:"file"`
	RetentionDays int    `mapstructure:"retention_days"` // 0 = no time prune (row cap still applies)
}

const MaxLogRetentionDays = 365

func ClampRetentionDays(n int) int {
	if n < 0 {
		return 0
	}
	if n > MaxLogRetentionDays {
		return MaxLogRetentionDays
	}
	return n
}

type WebConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Host      string `mapstructure:"host"`
	Port      int    `mapstructure:"port"`
	Role      string `mapstructure:"role"`       // control (default) | node
	PublicURL string `mapstructure:"public_url"` // https://panel.example.com — OAuth redirect base
	AccessKey string `mapstructure:"access_key"` // secret URL prefix; panel lives at /<access_key>/
}

const (
	WebRoleControl = "control"
	WebRoleNode    = "node"
)

func (w WebConfig) IsNode() bool {
	return w.Role == WebRoleNode
}

type PollerConfig struct {
	IntervalSec       int `mapstructure:"interval_sec"`
	MetricRetention   int `mapstructure:"metric_retention"` // snapshots per server
	UptimeIntervalSec int `mapstructure:"uptime_interval_sec"`
}

type EmailConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	SMTPHost string `mapstructure:"smtp_host"`
	SMTPPort int    `mapstructure:"smtp_port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	From     string `mapstructure:"from"`
}

func DefaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "xmanager")
}

func Load() (*Config, error) {
	dataDir := DefaultDataDir()

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("creating config directory: %w", err)
	}

	configPath := filepath.Join(dataDir, "config.yaml")

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.SetDefault("ai.provider", "ollama")
	viper.SetDefault("ai.model", "llama3")
	viper.SetDefault("ai.ollama_host", "http://localhost:11434")
	viper.SetDefault("ai.max_log_lines", 200)
	viper.SetDefault("telegram.enabled", false)
	viper.SetDefault("ui.theme", "dark")
	viper.SetDefault("ui.refresh_rate", 5)
	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.file", filepath.Join(dataDir, "xmanager.log"))
	viper.SetDefault("log.retention_days", 30)
	viper.SetDefault("web.enabled", false)
	viper.SetDefault("web.host", "127.0.0.1")
	viper.SetDefault("web.port", 8080)
	viper.SetDefault("web.role", "control")
	viper.SetDefault("web.public_url", "")
	viper.SetDefault("poller.interval_sec", 30)
	viper.SetDefault("poller.metric_retention", 288)
	viper.SetDefault("poller.uptime_interval_sec", 60)
	viper.SetDefault("email.enabled", false)
	viper.SetDefault("email.smtp_port", 587)

	viper.SetEnvPrefix("XMANAGER")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("reading config: %w", err)
			}
		}
	}

	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.DataDir = dataDir
	cfg.ConfigPath = configPath
	if cfg.Web.Role == "" {
		cfg.Web.Role = WebRoleControl
	}
	cfg.Log.RetentionDays = ClampRetentionDays(cfg.Log.RetentionDays)

	return cfg, nil
}

func Save(cfg *Config) error {
	viper.Set("ai.provider", cfg.AI.Provider)
	viper.Set("ai.model", cfg.AI.Model)
	viper.Set("ai.api_key", cfg.AI.APIKey)
	viper.Set("ai.endpoint", cfg.AI.Endpoint)
	viper.Set("ai.ollama_host", cfg.AI.OllamaHost)
	viper.Set("ai.whisper_key", cfg.AI.WhisperKey)
	viper.Set("ai.max_log_lines", cfg.AI.MaxLogLines)
	viper.Set("telegram.bot_token", cfg.Telegram.BotToken)
	viper.Set("telegram.chat_id", cfg.Telegram.ChatID)
	viper.Set("telegram.enabled", cfg.Telegram.Enabled)
	viper.Set("ui.theme", cfg.UI.Theme)
	viper.Set("ui.refresh_rate", cfg.UI.RefreshRate)
	cfg.Log.RetentionDays = ClampRetentionDays(cfg.Log.RetentionDays)
	viper.Set("log.level", cfg.Log.Level)
	viper.Set("log.file", cfg.Log.File)
	viper.Set("log.retention_days", cfg.Log.RetentionDays)
	viper.Set("web.enabled", cfg.Web.Enabled)
	viper.Set("web.host", cfg.Web.Host)
	viper.Set("web.port", cfg.Web.Port)
	viper.Set("web.role", cfg.Web.Role)
	viper.Set("web.public_url", cfg.Web.PublicURL)
	viper.Set("web.access_key", cfg.Web.AccessKey)
	viper.Set("poller.interval_sec", cfg.Poller.IntervalSec)
	viper.Set("poller.metric_retention", cfg.Poller.MetricRetention)
	viper.Set("poller.uptime_interval_sec", cfg.Poller.UptimeIntervalSec)
	viper.Set("email.enabled", cfg.Email.Enabled)
	viper.Set("email.smtp_host", cfg.Email.SMTPHost)
	viper.Set("email.smtp_port", cfg.Email.SMTPPort)
	viper.Set("email.username", cfg.Email.Username)
	viper.Set("email.password", cfg.Email.Password)
	viper.Set("email.from", cfg.Email.From)

	if err := viper.WriteConfigAs(cfg.ConfigPath); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return os.Chmod(cfg.ConfigPath, 0600)
}

func Reset() error {
	dataDir := DefaultDataDir()
	configPath := filepath.Join(dataDir, "config.yaml")
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("Configuration reset successfully.")
	return nil
}
