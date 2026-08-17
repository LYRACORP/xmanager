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
		"apt-unlock": true, "base": true, "docker": true, "linux-harden": true, "nodejs": true,
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
		if r.ID == "apt-unlock" {
			if r.CanUninstall() {
				t.Fatal("apt-unlock should not have uninstall steps")
			}
			continue
		}
		if !r.CanUninstall() {
			t.Fatalf("recipe %s missing uninstall_steps", r.ID)
		}
	}
}

func TestLinuxHardenRecipe(t *testing.T) {
	r, ok := ByID("linux-harden")
	if !ok {
		t.Fatal("linux-harden missing")
	}
	if r.UninstallCommandCount() == 0 {
		t.Fatal("linux-harden needs uninstall commands")
	}
	joined := ""
	for _, s := range r.Steps {
		for _, c := range s.Commands {
			joined += c + "\n"
		}
	}
	for _, needle := range []string{
		"99-xmanager-harden.conf",
		"ufw --force enable",
		"fail2ban",
		"PermitEmptyPasswords no",
		"sshd -t",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("hardening install missing %q", needle)
		}
	}
	if strings.Contains(joined, "PermitRootLogin no") || strings.Contains(joined, "PasswordAuthentication no") {
		t.Fatal("linux-harden must not disable root or password SSH")
	}
	if strings.Contains(joined, "ip_forward =") || strings.Contains(joined, "ip_forward=") {
		t.Fatal("linux-harden must not set ip_forward")
	}
}

func TestDestructiveFlags(t *testing.T) {
	want := map[string]bool{"docker": true, "postgres": true, "portainer": true, "python": true}
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Destructive != want[r.ID] {
			t.Fatalf("%s destructive=%v want %v", r.ID, r.Destructive, want[r.ID])
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
