package recipes

import (
	"strings"
	"testing"
)

func TestAllRecipesLoad(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"apt-unlock": true, "base": true, "docker": true, "nodejs": true,
		"pgadmin4": true, "portainer": true, "postgres": true, "python": true,
	}
	if len(all) != len(want) {
		t.Fatalf("got %d recipes, want %d", len(all), len(want))
	}
	for _, r := range all {
		if !want[r.ID] {
			t.Fatalf("unexpected recipe id %q", r.ID)
		}
		if r.Name == "" || len(r.Steps) == 0 {
			t.Fatalf("recipe %s incomplete: %+v", r.ID, r)
		}
		if r.CommandCount() == 0 {
			t.Fatalf("recipe %s has no commands", r.ID)
		}
	}
}

func TestMaterializePostgresSecrets(t *testing.T) {
	r, ok := ByID("postgres")
	if !ok {
		t.Fatal("postgres recipe missing")
	}
	out, creds, err := Materialize(r)
	if err != nil {
		t.Fatal(err)
	}
	if creds == "" {
		t.Fatal("expected credentials summary")
	}
	joined := ""
	for _, s := range out.Steps {
		for _, c := range s.Commands {
			joined += c
		}
	}
	for _, key := range []string{
		"__XM_PG_SUPERPASS__", "__XM_PG_APPPASS__", "__XM_PG_SUPERUSER__",
	} {
		if strings.Contains(joined, key) {
			t.Fatalf("placeholder %s still present", key)
		}
	}
	if !strings.Contains(joined, "xm_admin") {
		t.Fatal("expected generated superuser name")
	}
}
