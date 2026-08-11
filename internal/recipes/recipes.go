package recipes

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed data/*.yaml
var recipeFS embed.FS

// Step is one named group of remote shell commands.
type Step struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Commands    []string `yaml:"commands"`
}

// Recipe is an installable host setup script.
type Recipe struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Requires    []string `yaml:"requires"`
	Steps       []Step   `yaml:"steps"`
}

// ProgressFunc reports overall progress while a recipe runs.
type ProgressFunc func(pct float64, detail string)

// All loads and returns embedded recipes sorted by name.
func All() ([]Recipe, error) {
	entries, err := recipeFS.ReadDir("data")
	if err != nil {
		return nil, err
	}
	out := make([]Recipe, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		raw, err := recipeFS.ReadFile("data/" + e.Name())
		if err != nil {
			return nil, err
		}
		var r Recipe
		if err := yaml.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if r.ID == "" {
			return nil, fmt.Errorf("%s: missing id", e.Name())
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// ByID returns a recipe by id.
func ByID(id string) (Recipe, bool) {
	all, err := All()
	if err != nil {
		return Recipe{}, false
	}
	for _, r := range all {
		if r.ID == id {
			return r, true
		}
	}
	return Recipe{}, false
}

// Materialize replaces secret placeholders with generated values.
// Returns the recipe copy and a human-readable credentials summary (may be empty).
func Materialize(r Recipe) (Recipe, string, error) {
	repl := map[string]string{}
	var creds []string

	need := func(key, label string, gen func() (string, error)) error {
		if !recipeNeeds(r, key) {
			return nil
		}
		v, err := gen()
		if err != nil {
			return err
		}
		repl[key] = v
		creds = append(creds, label+": "+v)
		return nil
	}

	if err := need("__XM_PGADMIN_EMAIL__", "pgAdmin email", func() (string, error) {
		return "admin@localhost", nil
	}); err != nil {
		return r, "", err
	}
	if err := need("__XM_PGADMIN_PASSWORD__", "pgAdmin password", randomPassword); err != nil {
		return r, "", err
	}
	if err := need("__XM_PG_SUPERUSER__", "PostgreSQL superuser", func() (string, error) {
		return "xm_admin", nil
	}); err != nil {
		return r, "", err
	}
	if err := need("__XM_PG_SUPERPASS__", "PostgreSQL superuser password", randomPassword); err != nil {
		return r, "", err
	}
	if err := need("__XM_PG_APPUSER__", "PostgreSQL app user", func() (string, error) {
		return "xm_app", nil
	}); err != nil {
		return r, "", err
	}
	if err := need("__XM_PG_APPPASS__", "PostgreSQL app password", randomPassword); err != nil {
		return r, "", err
	}
	if err := need("__XM_PG_DATABASE__", "PostgreSQL database", func() (string, error) {
		return "xm_app", nil
	}); err != nil {
		return r, "", err
	}

	out := r
	out.Steps = make([]Step, len(r.Steps))
	for i, s := range r.Steps {
		ns := s
		ns.Commands = make([]string, len(s.Commands))
		for j, c := range s.Commands {
			ns.Commands[j] = replaceAll(c, repl)
		}
		out.Steps[i] = ns
	}
	return out, strings.Join(creds, "\n"), nil
}

func recipeNeeds(r Recipe, key string) bool {
	for _, s := range r.Steps {
		for _, c := range s.Commands {
			if strings.Contains(c, key) {
				return true
			}
		}
	}
	return false
}

func replaceAll(s string, repl map[string]string) string {
	for k, v := range repl {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CommandCount returns total remote commands in a recipe.
func (r Recipe) CommandCount() int {
	n := 0
	for _, s := range r.Steps {
		n += len(s.Commands)
	}
	return n
}
