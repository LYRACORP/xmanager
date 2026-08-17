package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/storage"
)

type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) ListModels(context.Context) ([]string, error) { return nil, nil }
func (fakeProvider) Chat(context.Context, []Message, ...Option) (string, error) {
	return "ok", nil
}
func (fakeProvider) ChatStream(context.Context, []Message, chan<- string, ...Option) error {
	return nil
}
func (f fakeProvider) ChatWithTools(_ context.Context, messages []Message, _ []ops.ToolSpec, _ ...Option) (ChatTurn, error) {
	last := messages[len(messages)-1]
	if last.Role == RoleTool {
		return ChatTurn{Content: "done with tools"}, nil
	}
	if strings.Contains(last.Content, "list") {
		return ChatTurn{ToolCalls: []ToolCall{{ID: "1", Name: "list_servers", Args: "{}"}}}, nil
	}
	return ChatTurn{Content: "hello"}, nil
}

func TestAgentPlainReply(t *testing.T) {
	cat := ops.New(nil, nil)
	a := NewAgent(fakeProvider{}, cat, "")
	text, err := a.Send(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("got %q", text)
	}
}

func TestAgentToolThenText(t *testing.T) {
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cat := ops.New(db, nil)
	a := NewAgent(fakeProvider{}, cat, "")
	text, err := a.Send(context.Background(), "list my servers")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "done") {
		t.Fatalf("got %q", text)
	}
}

func TestConfirmNeededErrorAs(t *testing.T) {
	err := &ops.ConfirmNeededError{Pending: ops.PendingConfirm{Tool: "x"}}
	var need *ops.ConfirmNeededError
	if !errors.As(err, &need) {
		t.Fatal("as")
	}
}
