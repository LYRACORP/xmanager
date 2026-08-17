package ops

import (
	"context"
	"encoding/json"
	"fmt"
)

// Risk classifies a tool for confirmation policy.
type Risk string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskDestructive Risk = "destructive"
)

// Tool is one callable operation shared by MCP, the in-app agent, and workflows.
type Tool struct {
	Name        string
	Description string
	Group       string
	Risk        Risk
	InputSchema map[string]any
	Handler     Handler
}

// Handler executes a tool. args are JSON-decoded objects (numbers as float64).
type Handler func(ctx context.Context, c *Catalog, args map[string]any) (string, error)

// CallOptions controls confirmation and similar policy.
type CallOptions struct {
	AllowDestructive bool
}

// PendingConfirm is returned (via ConfirmNeededError) when a destructive tool
// needs an explicit operator approval before running.
type PendingConfirm struct {
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Risk    Risk           `json:"risk"`
	Summary string         `json:"summary"`
}

// ConfirmNeededError signals that Call must be retried with AllowDestructive.
type ConfirmNeededError struct {
	Pending PendingConfirm
}

func (e *ConfirmNeededError) Error() string {
	return fmt.Sprintf("confirmation required for %s: %s", e.Pending.Tool, e.Pending.Summary)
}

func jsonText(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(b), nil
}
