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
	DataDir    string     `mapstructure:"-"`
	ConfigPath string     `mapstructure:"-"`
	AI         AIConfig   `mapstructure:"ai"`
	Telegram   TGConfig   `mapstructure:"telegram"`
	UI         UIConfig   `mapstructure:"ui"`
	Log        LogConfig  `mapstructure:"log"`
}

type AIConfig struct {
	Provider    string `mapstructure:"provider"`    // openai, anthropic, ollama
	Model       string `mapstructure:"model"`
	APIKey      string `mapstructure:"api_key"`
	OllamaHost  string `mapstructure:"ollama_host"`
	MaxLogLines int    `mapstructure:"max_log_lines"`
}

type TGConfig struct {
	BotToken string `mapstructure:"bot_token"`
	ChatID   string `mapstructure:"chat_id"`
	Enabled  bool   `mapstructure:"enabled"`
}

type UIConfig struct {
	Theme        string `mapstructure:"theme"` // dark, light
	RefreshRate  int    `mapstructure:"refresh_rate"`
}

type LogConfig struct {
	Level string `mapstructure:"level"` // debug, info, warn, error
	File  string `mapstructure:"file"`
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

	return cfg, nil
}

func Save(cfg *Config) error {
	viper.Set("ai.provider", cfg.AI.Provider)
	viper.Set("ai.model", cfg.AI.Model)
	viper.Set("ai.api_key", cfg.AI.APIKey)
	viper.Set("ai.ollama_host", cfg.AI.OllamaHost)
	viper.Set("ai.max_log_lines", cfg.AI.MaxLogLines)
	viper.Set("telegram.bot_token", cfg.Telegram.BotToken)
	viper.Set("telegram.chat_id", cfg.Telegram.ChatID)
	viper.Set("telegram.enabled", cfg.Telegram.Enabled)
	viper.Set("ui.theme", cfg.UI.Theme)
	viper.Set("ui.refresh_rate", cfg.UI.RefreshRate)
	viper.Set("log.level", cfg.Log.Level)
	viper.Set("log.file", cfg.Log.File)

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
