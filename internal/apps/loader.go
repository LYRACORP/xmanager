package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Variable describes a user-configurable parameter in a one-click app template.
type Variable struct {
	ID           string `yaml:"id"`
	Label        string `yaml:"label"`
	Description  string `yaml:"description"`
	DefaultValue string `yaml:"defaultValue"`
	ValidRegex   string `yaml:"validRegex"`
}

// App is the parsed representation of a CapRover-style one-click app YAML.
type App struct {
	ID          string // filename without .yml
	DisplayName string
	Description string
	IsOfficial  bool
	Variables   []Variable
	ComposeYAML string // raw services block rendered as docker-compose
	Instructions struct {
		Start string
		End   string
	}
}

// Summary is a lightweight catalog entry for listing templates.
type Summary struct {
	ID          string
	DisplayName string
	Description string
	IsOfficial  bool
}

// raw YAML shape matching CapRover one-click-app format (v4)
type rawApp struct {
	CaptainVersion int                    `yaml:"captainVersion"`
	Services       map[string]interface{} `yaml:"services"`
	OneClickApp    struct {
		Variables    []Variable `yaml:"variables"`
		Instructions struct {
			Start string `yaml:"start"`
			End   string `yaml:"end"`
		} `yaml:"instructions"`
		DisplayName string `yaml:"displayName"`
		IsOfficial  bool   `yaml:"isOfficial"`
		Description string `yaml:"description"`
	} `yaml:"caproverOneClickApp"`
}

// Loader finds and parses one-click app YAML files.
type Loader struct {
	dirs []string
}

// DefaultDirs are the standard CapRover one-click search paths (relative to cwd).
func DefaultDirs() []string {
	return []string{
		"apps",
		"apps/caprover",
	}
}

// NewLoader returns a Loader that searches dirs in order (first match wins).
func NewLoader(dirs ...string) *Loader {
	if len(dirs) == 0 {
		dirs = DefaultDirs()
	}
	return &Loader{dirs: dirs}
}

// DefaultLoader uses DefaultDirs.
func DefaultLoader() *Loader {
	return NewLoader(DefaultDirs()...)
}

// List returns the IDs (filename without .yml) of all available one-click apps.
func (l *Loader) List() ([]string, error) {
	seen := make(map[string]bool)
	var ids []string
	for _, dir := range l.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".yml")
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// ListSummaries returns catalog metadata for each template (best-effort parse).
func (l *Loader) ListSummaries() ([]Summary, error) {
	ids, err := l.List()
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(ids))
	for _, id := range ids {
		app, err := l.Load(id)
		if err != nil {
			out = append(out, Summary{ID: id, DisplayName: id})
			continue
		}
		name := app.DisplayName
		if name == "" {
			name = id
		}
		out = append(out, Summary{
			ID:          id,
			DisplayName: name,
			Description: app.Description,
			IsOfficial:  app.IsOfficial,
		})
	}
	return out, nil
}

// Load parses an app by ID (filename without .yml).
func (l *Loader) Load(name string) (*App, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid app id")
	}
	for _, dir := range l.dirs {
		path := filepath.Join(dir, name+".yml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		app, err := parseApp(data)
		if err != nil {
			return nil, err
		}
		app.ID = name
		if app.DisplayName == "" {
			app.DisplayName = name
		}
		return app, nil
	}
	return nil, fmt.Errorf("app %q not found in any search path", name)
}

// Render substitutes user-provided variable values into the compose YAML and
// returns the rendered compose document ready for docker compose up.
func (l *Loader) Render(name string, values map[string]string) (string, error) {
	app, err := l.Load(name)
	if err != nil {
		return "", err
	}

	compose := app.ComposeYAML
	for _, v := range app.Variables {
		val, ok := values[v.ID]
		if !ok || val == "" {
			val = v.DefaultValue
		}
		compose = strings.ReplaceAll(compose, v.ID, val)
		// CapRover also uses $$cap_appname style without doubling in some files
		short := strings.TrimPrefix(v.ID, "$$")
		if short != v.ID {
			compose = strings.ReplaceAll(compose, "$$"+short, val)
		}
	}
	return compose, nil
}

func parseApp(data []byte) (*App, error) {
	var raw rawApp
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing app yaml: %w", err)
	}

	composeYAML := ""
	if len(raw.Services) > 0 {
		doc := map[string]interface{}{
			"services": raw.Services,
		}
		b, err := yaml.Marshal(doc)
		if err == nil {
			composeYAML = string(b)
		}
	}

	app := &App{
		DisplayName: raw.OneClickApp.DisplayName,
		Description: raw.OneClickApp.Description,
		IsOfficial:  raw.OneClickApp.IsOfficial,
		Variables:   raw.OneClickApp.Variables,
		ComposeYAML: composeYAML,
	}
	app.Instructions.Start = raw.OneClickApp.Instructions.Start
	app.Instructions.End = raw.OneClickApp.Instructions.End

	return app, nil
}
