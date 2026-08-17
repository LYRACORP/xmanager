package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ops"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role       Role      `json:"role"`
	Content    string    `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Name       string    `json:"name,omitempty"`
}

type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Args string `json:"args"`
}

type ChatTurn struct {
	Content   string
	ToolCalls []ToolCall
}

type ChatOptions struct {
	MaxTokens   int
	Temperature float64
	Model       string
}

type Option func(*ChatOptions)

func WithMaxTokens(n int) Option {
	return func(o *ChatOptions) { o.MaxTokens = n }
}

func WithTemperature(t float64) Option {
	return func(o *ChatOptions) { o.Temperature = t }
}

func WithModel(m string) Option {
	return func(o *ChatOptions) { o.Model = m }
}

type Provider interface {
	Chat(ctx context.Context, messages []Message, opts ...Option) (string, error)
	ChatStream(ctx context.Context, messages []Message, out chan<- string, opts ...Option) error
	ChatWithTools(ctx context.Context, messages []Message, tools []ops.ToolSpec, opts ...Option) (ChatTurn, error)
	Name() string
	ListModels(ctx context.Context) ([]string, error)
}

type ProviderConfig struct {
	Type     string
	APIKey   string
	Model    string
	Endpoint string
}

func DefaultEndpoint(typ string) string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "openai":
		return "https://api.openai.com/v1"
	case "grok":
		return "https://api.x.ai/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "lmstudio":
		return "http://127.0.0.1:1234/v1"
	case "anthropic":
		return "https://api.anthropic.com"
	case "ollama":
		return "http://localhost:11434"
	default:
		return ""
	}
}

func ProviderConfigFromAI(c config.AIConfig) ProviderConfig {
	typ := strings.ToLower(strings.TrimSpace(c.Provider))
	if typ == "" {
		typ = "ollama"
	}
	ep := strings.TrimSpace(c.Endpoint)
	if ep == "" && typ == "ollama" {
		ep = strings.TrimSpace(c.OllamaHost)
	}
	if ep == "" {
		ep = DefaultEndpoint(typ)
	}
	return ProviderConfig{Type: typ, APIKey: c.APIKey, Model: c.Model, Endpoint: ep}
}

func NewProvider(cfg ProviderConfig) (Provider, error) {
	typ := strings.ToLower(strings.TrimSpace(cfg.Type))
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint(typ)
	}
	switch typ {
	case "openai", "grok", "gemini", "deepseek", "openrouter", "lmstudio":
		p := NewOpenAI(cfg)
		p.name = typ
		return p, nil
	case "anthropic":
		return NewAnthropic(cfg), nil
	case "ollama":
		return NewOllama(cfg), nil
	default:
		return nil, fmt.Errorf("unknown AI provider: %s", cfg.Type)
	}
}

func defaultOptions(opts []Option) ChatOptions {
	o := ChatOptions{
		MaxTokens:   4096,
		Temperature: 0.7,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
