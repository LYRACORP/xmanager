package recipes

import "sort"

// InstalledRef is a recorded TUI recipe install used to compute uninstall order.
type InstalledRef struct {
	RecipeID  string
	Installed int64 // unix nanos; newer first when ties are broken by requires
}

// UninstallOrder returns recipes that can be uninstalled, dependents first,
// then reverse install time (newest first). Recipes without uninstall_steps are skipped.
func UninstallOrder(installed []InstalledRef, all []Recipe) []Recipe {
	byID := make(map[string]Recipe, len(all))
	for _, r := range all {
		byID[r.ID] = r
	}
	when := make(map[string]int64, len(installed))
	ids := make([]string, 0, len(installed))
	seen := map[string]bool{}
	for _, inst := range installed {
		r, ok := byID[inst.RecipeID]
		if !ok || !r.CanUninstall() || seen[inst.RecipeID] {
			continue
		}
		seen[inst.RecipeID] = true
		when[inst.RecipeID] = inst.Installed
		ids = append(ids, inst.RecipeID)
	}
	sort.SliceStable(ids, func(i, j int) bool {
		return when[ids[i]] > when[ids[j]]
	})

	remaining := make(map[string]bool, len(ids))
	for _, id := range ids {
		remaining[id] = true
	}
	out := make([]Recipe, 0, len(ids))
	for len(remaining) > 0 {
		picked := ""
		for _, id := range ids {
			if !remaining[id] {
				continue
			}
			if hasInstalledDependent(id, remaining, byID) {
				continue
			}
			picked = id
			break
		}
		if picked == "" {
			for _, id := range ids {
				if remaining[id] {
					picked = id
					break
				}
			}
		}
		out = append(out, byID[picked])
		delete(remaining, picked)
	}
	return out
}

func hasInstalledDependent(id string, remaining map[string]bool, byID map[string]Recipe) bool {
	for other := range remaining {
		if other == id {
			continue
		}
		for _, req := range byID[other].Requires {
			if req == id {
				return true
			}
		}
	}
	return false
}
