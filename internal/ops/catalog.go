package ops

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/lyracorp/xmanager/internal/ssh"
	"gorm.io/gorm"
)

// Completer generates text from a prompt (used by analyze_server).
type Completer func(ctx context.Context, prompt string) (string, error)

// Catalog is the in-process tool registry used by MCP, chat, and workflows.
type Catalog struct {
	DB        *gorm.DB
	Pool      *ssh.Pool
	Completer Completer

	mu    sync.RWMutex
	tools map[string]Tool
}

func New(db *gorm.DB, pool *ssh.Pool) *Catalog {
	c := &Catalog{DB: db, Pool: pool, tools: make(map[string]Tool)}
	c.registerCore()
	c.registerDocker()
	c.registerProjects()
	c.registerFiles()
	c.registerPackages()
	c.registerDatabases()
	c.registerBackup()
	c.registerSecurity()
	c.registerDomains()
	c.registerProcess()
	c.registerK8s()
	return c
}

func (c *Catalog) add(t Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tools[t.Name] = t
}

func (c *Catalog) Get(name string) (Tool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tools[name]
	return t, ok
}

func (c *Catalog) List() []Tool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Tool, 0, len(c.tools))
	for _, t := range c.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group == out[j].Group {
			return out[i].Name < out[j].Name
		}
		return out[i].Group < out[j].Group
	})
	return out
}

// Specs returns name/description/schema for LLM tool calling.
func (c *Catalog) Specs() []ToolSpec {
	list := c.List()
	out := make([]ToolSpec, 0, len(list))
	for _, t := range list {
		out = append(out, ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out
}

// ToolSpec is the provider-agnostic tool definition.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema any
}

func (c *Catalog) Call(ctx context.Context, name string, args map[string]any, opts CallOptions) (string, error) {
	if args == nil {
		args = map[string]any{}
	}
	t, ok := c.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	if t.Risk == RiskDestructive && !opts.AllowDestructive {
		return "", &ConfirmNeededError{Pending: PendingConfirm{
			Tool:    t.Name,
			Args:    args,
			Risk:    t.Risk,
			Summary: t.Description,
		}}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return t.Handler(ctx, c, args)
}
