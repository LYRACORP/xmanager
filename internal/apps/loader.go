package apps

import (
	"fmt"
	"os"
	"path/filepath"
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

// NewLoader returns a Loader that searches dirs in order (first match wins).
func NewLoader(dirs ...string) *Loader {
	return &Loader{dirs: dirs}
}

// List returns the names (display names) of all available one-click apps.
func (l *Loader) List() ([]string, error) {
	seen := make(map[string]bool)
	var names []string
	for _, dir := range l.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".yml") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".yml")
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names, nil
}

// Load parses an app by name (filename without .yml).
func (l *Loader) Load(name string) (*App, error) {
	for _, dir := range l.dirs {
		path := filepath.Join(dir, name+".yml")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseApp(data)
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
	// apply defaults first, then provided values
	for _, v := range app.Variables {
		val, ok := values[v.ID]
		if !ok || val == "" {
			val = v.DefaultValue
		}
		compose = strings.ReplaceAll(compose, v.ID, val)
	}
	return compose, nil
}

func parseApp(data []byte) (*App, error) {
	var raw rawApp
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing app yaml: %w", err)
	}

	// Re-marshal just the services block so callers get a clean compose snippet.
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
