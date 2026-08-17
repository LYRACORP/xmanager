package recipes

import "testing"

func TestUninstallOrderDependentsFirst(t *testing.T) {
	all := []Recipe{
		{ID: "docker", Name: "Docker", UninstallSteps: []Step{{Name: "rm", Commands: []string{"true"}}}},
		{ID: "portainer", Name: "Portainer", Requires: []string{"docker"}, UninstallSteps: []Step{{Name: "rm", Commands: []string{"true"}}}},
		{ID: "base", Name: "Base", UninstallSteps: []Step{{Name: "rm", Commands: []string{"true"}}}},
		{ID: "apt-unlock", Name: "Unlock"},
	}
	got := UninstallOrder([]InstalledRef{
		{RecipeID: "docker", Installed: 1},
		{RecipeID: "portainer", Installed: 2},
		{RecipeID: "base", Installed: 3},
		{RecipeID: "apt-unlock", Installed: 4},
	}, all)
	if len(got) != 3 {
		t.Fatalf("got %d recipes, want 3 (skip apt-unlock)", len(got))
	}
	ids := make([]string, len(got))
	for i, r := range got {
		ids[i] = r.ID
	}
	portainerAt, dockerAt := -1, -1
	for i, id := range ids {
		if id == "portainer" {
			portainerAt = i
		}
		if id == "docker" {
			dockerAt = i
		}
	}
	if portainerAt < 0 || dockerAt < 0 {
		t.Fatalf("missing docker/portainer: %v", ids)
	}
	if portainerAt > dockerAt {
		t.Fatalf("portainer should uninstall before docker: %v", ids)
	}
}

func TestUninstallMissingRecipeSkipped(t *testing.T) {
	got := UninstallOrder([]InstalledRef{{RecipeID: "nope", Installed: 1}}, []Recipe{
		{ID: "base", UninstallSteps: []Step{{Commands: []string{"true"}}}},
	})
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}
