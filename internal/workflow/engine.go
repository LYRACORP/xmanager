package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	X      float64        `json:"x"`
	Y      float64        `json:"y"`
	Config map[string]any `json:"config"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Port string `json:"port,omitempty"` // true/false for if
}

type StepLog struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

type Engine struct {
	DB  *gorm.DB
	Cat *ops.Catalog
}

func New(db *gorm.DB, cat *ops.Catalog) *Engine {
	e := &Engine{DB: db, Cat: cat}
	ops.SetWorkflowRunner(func(id uint) (string, error) {
		run, err := e.Run(context.Background(), id, "tool")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("run %d %s", run.ID, run.Status), nil
	})
	return e
}

func (e *Engine) HasDestructive(g Graph) bool {
	if e.Cat == nil {
		return false
	}
	for _, n := range g.Nodes {
		if t, ok := e.Cat.Get(n.Type); ok && t.Risk == ops.RiskDestructive {
			return true
		}
	}
	return false
}

func ParseGraph(raw string) (Graph, error) {
	var g Graph
	if strings.TrimSpace(raw) == "" {
		return g, nil
	}
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return g, err
	}
	return g, nil
}

func (e *Engine) Run(ctx context.Context, workflowID uint, trigger string) (*storage.WorkflowRun, error) {
	var wf storage.Workflow
	if err := e.DB.First(&wf, workflowID).Error; err != nil {
		return nil, err
	}
	if wf.HasDestructive && !wf.Enabled {
		return nil, fmt.Errorf("workflow %q includes destructive steps and is disabled", wf.Name)
	}
	g, err := ParseGraph(wf.GraphJSON)
	if err != nil {
		return nil, err
	}
	run := storage.WorkflowRun{WorkflowID: wf.ID, Status: "running", Trigger: trigger, StartedAt: time.Now()}
	if err := e.DB.Create(&run).Error; err != nil {
		return nil, err
	}
	logs, runErr := e.execute(ctx, g)
	raw, _ := json.Marshal(logs)
	now := time.Now()
	run.LogJSON = string(raw)
	run.FinishedAt = &now
	if runErr != nil {
		run.Status = "error"
		run.LogJSON = string(append(raw, []byte("\n"+runErr.Error())...))
	} else {
		run.Status = "ok"
	}
	_ = e.DB.Save(&run).Error
	return &run, runErr
}

func (e *Engine) execute(ctx context.Context, g Graph) ([]StepLog, error) {
	byID := map[string]Node{}
	incoming := map[string][]Edge{}
	outgoing := map[string][]Edge{}
	indeg := map[string]int{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
		indeg[n.ID] = 0
	}
	for _, e := range g.Edges {
		incoming[e.To] = append(incoming[e.To], e)
		outgoing[e.From] = append(outgoing[e.From], e)
		indeg[e.To]++
	}
	outputs := map[string]string{}
	var logs []StepLog
	queue := []string{}
	for id, d := range indeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		n := byID[id]
		out, err := e.runNode(ctx, n, outputs)
		lg := StepLog{ID: id, Type: n.Type, Output: out}
		if err != nil {
			lg.Error = err.Error()
			logs = append(logs, lg)
			cont := false
			if n.Config != nil {
				switch v := n.Config["continue"].(type) {
				case bool:
					cont = v
				case string:
					cont = v == "true" || v == "1"
				}
			}
			if !cont {
				return logs, err
			}
			outputs[id] = out
			continue
		}
		outputs[id] = out
		logs = append(logs, lg)
		for _, edge := range outgoing[id] {
			if n.Type == "if" {
				ok := strings.Contains(strings.ToLower(out), "true") || strings.TrimSpace(out) == "1"
				if edge.Port == "false" && ok {
					continue
				}
				if edge.Port == "true" && !ok {
					continue
				}
			}
			queue = append(queue, edge.To)
		}
	}
	return logs, nil
}

func (e *Engine) runNode(ctx context.Context, n Node, outputs map[string]string) (string, error) {
	cfg := map[string]any{}
	for k, v := range n.Config {
		if s, ok := v.(string); ok {
			cfg[k] = applyTemplate(s, outputs)
		} else {
			cfg[k] = v
		}
	}
	switch n.Type {
	case "if":
		expr := strings.ToLower(fmt.Sprint(cfg["expr"]))
		for _, out := range outputs {
			if expr != "" && strings.Contains(strings.ToLower(out), expr) {
				return "true", nil
			}
		}
		if expr == "" || expr == "true" {
			return "true", nil
		}
		return "false", nil
	case "delay":
		sec := 1
		switch v := cfg["seconds"].(type) {
		case float64:
			sec = int(v)
		case int:
			sec = v
		}
		if sec < 0 {
			sec = 0
		}
		if sec > 60 {
			sec = 60
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(sec) * time.Second):
			return fmt.Sprintf("slept %ds", sec), nil
		}
	case "foreach_server":
		out, err := e.Cat.Call(ctx, "list_servers", map[string]any{}, ops.CallOptions{AllowDestructive: false})
		return out, err
	default:
		if e.Cat == nil {
			return "", fmt.Errorf("no catalog")
		}
		if _, ok := e.Cat.Get(n.Type); !ok {
			return "", fmt.Errorf("unknown node type %s", n.Type)
		}
		return e.Cat.Call(ctx, n.Type, cfg, ops.CallOptions{AllowDestructive: true})
	}
}

func applyTemplate(s string, outputs map[string]string) string {
	for id, out := range outputs {
		s = strings.ReplaceAll(s, "{{steps."+id+".output}}", out)
	}
	return s
}

func (e *Engine) TriggerChat(text string) {
	if e == nil || e.DB == nil {
		return
	}
	for _, wf := range ChatMatches(e.DB, text) {
		id := wf.ID
		go func() { _, _ = e.Run(context.Background(), id, "chat") }()
	}
}

func (e *Engine) TriggerAlert(text string) {
	if e == nil || e.DB == nil {
		return
	}
	var list []storage.Workflow
	_ = e.DB.Where("enabled = ? AND trigger = ?", true, "alert").Find(&list).Error
	for _, wf := range list {
		id := wf.ID
		go func() { _, _ = e.Run(context.Background(), id, "alert") }()
	}
}
