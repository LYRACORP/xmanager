package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ops"
)

const maxToolTurns = 12

// Event is one agent stream item for TUI/web.
type Event struct {
	Type    string              `json:"type"`
	Text    string              `json:"text"`
	Tool    string              `json:"tool,omitempty"`
	Confirm *ops.PendingConfirm `json:"confirm,omitempty"`
}

// Agent wraps a Provider with a tool-calling loop over an ops catalog.
type Agent struct {
	provider    Provider
	catalog     *ops.Catalog
	history     []Message
	pending     *ops.PendingConfirm
	extraSystem string
	lockedSID   uint
}

func NewAgent(p Provider, cat *ops.Catalog, extraSystem string) *Agent {
	sys := BuildAgentSystemPrompt(cat)
	if extraSystem != "" {
		sys += "\n\n" + extraSystem
	}
	return &Agent{
		provider:    p,
		catalog:     cat,
		extraSystem: extraSystem,
		history: []Message{
			{Role: RoleSystem, Content: sys},
		},
	}
}

func (a *Agent) SetLockedServer(id uint) { a.lockedSID = id }

func (a *Agent) Pending() *ops.PendingConfirm { return a.pending }

func (a *Agent) Reset() {
	sys := BuildAgentSystemPrompt(a.catalog)
	if a.extraSystem != "" {
		sys += "\n\n" + a.extraSystem
	}
	a.history = []Message{{Role: RoleSystem, Content: sys}}
	a.pending = nil
}

func (a *Agent) History() []Message {
	out := make([]Message, len(a.history))
	copy(out, a.history)
	return out
}

func (a *Agent) Provider() Provider { return a.provider }

func (a *Agent) LoadHistory(msgs []Message) {
	if len(msgs) == 0 {
		return
	}
	sys := a.history[0]
	a.history = append([]Message{sys}, msgs...)
}

// Send runs one user turn (or confirms a pending destructive tool) and returns the assistant text.
func (a *Agent) Send(ctx context.Context, userMsg string, opts ...Option) (string, error) {
	var b strings.Builder
	err := a.Run(ctx, userMsg, func(ev Event) {
		switch ev.Type {
		case "text", "confirm", "error":
			if ev.Text != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(ev.Text)
			}
		}
	}, opts...)
	return b.String(), err
}

func (a *Agent) Run(ctx context.Context, userMsg string, emit func(Event), opts ...Option) error {
	if emit == nil {
		emit = func(Event) {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}

	if a.pending != nil {
		low := strings.ToLower(strings.TrimSpace(userMsg))
		if low == "y" || low == "yes" || low == "confirm" {
			pending := *a.pending
			a.pending = nil
			text, err := a.catalog.Call(ctx, pending.Tool, pending.Args, ops.CallOptions{AllowDestructive: true})
			if err != nil {
				emit(Event{Type: "error", Text: err.Error()})
				return err
			}
			a.history = append(a.history, Message{Role: RoleUser, Content: "Confirmed: " + pending.Tool})
			a.history = append(a.history, Message{Role: RoleAssistant, Content: text})
			emit(Event{Type: "text", Text: text})
			emit(Event{Type: "done"})
			return nil
		}
		a.pending = nil
		a.history = append(a.history, Message{Role: RoleUser, Content: "Cancelled destructive action " + a.toolNameSafe()})
		note := "Cancelled. I will not run that destructive action."
		a.history = append(a.history, Message{Role: RoleAssistant, Content: note})
		emit(Event{Type: "text", Text: note})
		emit(Event{Type: "done"})
		return nil
	}

	a.history = append(a.history, Message{Role: RoleUser, Content: userMsg})
	tools := a.catalog.Specs()

	for turn := 0; turn < maxToolTurns; turn++ {
		result, err := a.provider.ChatWithTools(ctx, a.history, tools, opts...)
		if err != nil {
			a.history = a.history[:len(a.history)-1]
			emit(Event{Type: "error", Text: err.Error()})
			return fmt.Errorf("agent chat: %w", err)
		}
		if len(result.ToolCalls) == 0 {
			a.history = append(a.history, Message{Role: RoleAssistant, Content: result.Content})
			emit(Event{Type: "text", Text: result.Content})
			emit(Event{Type: "done"})
			return nil
		}
		a.history = append(a.history, Message{Role: RoleAssistant, Content: result.Content, ToolCalls: result.ToolCalls})
		for _, tc := range result.ToolCalls {
			emit(Event{Type: "tool", Tool: tc.Name, Text: tc.Name})
			args := map[string]any{}
			if strings.TrimSpace(tc.Args) != "" {
				if err := json.Unmarshal([]byte(tc.Args), &args); err != nil {
					args = map[string]any{"_raw": tc.Args}
				}
			}
			if a.lockedSID > 0 {
				if _, ok := args["server_id"]; !ok {
					args["server_id"] = float64(a.lockedSID)
				}
			}
			out, err := a.catalog.Call(ctx, tc.Name, args, ops.CallOptions{})
			if err != nil {
				var need *ops.ConfirmNeededError
				if errors.As(err, &need) {
					a.pending = &need.Pending
					emit(Event{Type: "confirm", Confirm: &need.Pending, Text: need.Error()})
					emit(Event{Type: "done"})
					return nil
				}
				out = "error: " + err.Error()
			}
			a.history = append(a.history, Message{Role: RoleTool, Content: out, ToolCallID: tc.ID, Name: tc.Name})
		}
	}
	msg := "Stopped after too many tool turns."
	emit(Event{Type: "text", Text: msg})
	emit(Event{Type: "done"})
	return nil
}

func (a *Agent) toolNameSafe() string {
	if a.pending == nil {
		return ""
	}
	return a.pending.Tool
}

func BuildAgentSystemPrompt(cat *ops.Catalog) string {
	var b strings.Builder
	b.WriteString("You are XManager AI, an expert VPS orchestration assistant.\n")
	b.WriteString("XManager manages Linux servers exclusively over SSH — no daemons on managed hosts.\n")
	b.WriteString("Use tools to inspect and change servers. Prefer list_servers before guessing IDs.\n")
	b.WriteString("Never invent credentials. Summarize tool results clearly.\n")
	b.WriteString("Destructive tools require the operator to confirm in the UI; if a tool is blocked, explain what you wanted to do.\n")
	if cat != nil {
		b.WriteString("\nAvailable tools:\n")
		for _, t := range cat.List() {
			fmt.Fprintf(&b, "- %s [%s/%s]: %s\n", t.Name, t.Group, t.Risk, t.Description)
		}
	}
	return b.String()
}
