package project

import (
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
)

// Slug returns the filesystem-safe project name used in on-disk paths.
func Slug(name string) string {
	return sanitizeName(name)
}

// ProjectDir is the host directory for a project's files.
// Functions live under /opt/xmanager/functions/<slug>; everything else under /opt/xmanager/projects/<slug>.
func ProjectDir(proj storage.Project) string {
	slug := sanitizeName(proj.Name)
	if strings.EqualFold(proj.Type, "function") {
		return "/opt/xmanager/functions/" + slug
	}
	return "/opt/xmanager/projects/" + slug
}
