package ai

import (
	"testing"

	"github.com/lyracorp/xmanager/internal/config"
)

func TestDefaultEndpoint(t *testing.T) {
	if DefaultEndpoint("grok") != "https://api.x.ai/v1" {
		t.Fatal("grok")
	}
	if DefaultEndpoint("lmstudio") != "http://127.0.0.1:1234/v1" {
		t.Fatal("lmstudio")
	}
}

func TestProviderConfigFromAI(t *testing.T) {
	cfg := ProviderConfigFromAI(config.AIConfig{Provider: "OpenRouter", Model: "x", APIKey: "k"})
	if cfg.Type != "openrouter" {
		t.Fatalf("type %s", cfg.Type)
	}
	if cfg.Endpoint != DefaultEndpoint("openrouter") {
		t.Fatalf("endpoint %s", cfg.Endpoint)
	}
}

func TestNewProviderCompat(t *testing.T) {
	p, err := NewProvider(ProviderConfig{Type: "deepseek", APIKey: "k", Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "deepseek" {
		t.Fatalf("name %s", p.Name())
	}
}
