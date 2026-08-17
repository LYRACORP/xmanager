package ai

import (
	"context"
	"fmt"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/ops"
)

// Completer returns an ops.Completer that uses the live AI config (provider/model/key).
func Completer(cfg *config.Config) ops.Completer {
	return func(ctx context.Context, prompt string) (string, error) {
		if cfg == nil {
			return "", fmt.Errorf("AI config missing")
		}
		p, err := NewProvider(ProviderConfigFromAI(cfg.AI))
		if err != nil {
			return "", err
		}
		return p.Chat(ctx, []Message{{Role: RoleUser, Content: prompt}}, WithTemperature(0.1), WithMaxTokens(4096))
	}
}
