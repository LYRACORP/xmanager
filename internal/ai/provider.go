package ai

import (
	"context"
	"fmt"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
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
	Name() string
	ListModels(ctx context.Context) ([]string, error)
}

type ProviderConfig struct {
	Type     string // openai, anthropic, ollama
	APIKey   string
	Model    string
	Endpoint string
}

func NewProvider(cfg ProviderConfig) (Provider, error) {
	switch cfg.Type {
	case "openai":
		return NewOpenAI(cfg), nil
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
