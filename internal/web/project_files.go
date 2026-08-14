package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lyracorp/xmanager/internal/project"
	"github.com/lyracorp/xmanager/internal/storage"
)

const (
	maxFileEditBytes   = 10 << 20  // 10 MiB
	maxFileUploadBytes = 100 << 20 // 100 MiB
)

func (h *handler) projectFileRoot(p *storage.Project) string {
	return project.ProjectDir(*p)
}

// resolveUnderRoot joins root + rel and ensures the result stays inside root.
func resolveUnderRoot(root, rel string) (string, error) {
	root = filepath.Clean(root)
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return root, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("invalid path")
	}
	rel = strings.TrimPrefix(rel, "/")
	rel = filepath.ToSlash(rel)
	// Block parent escapes before join.
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") || strings.HasSuffix(rel, "/..") {
		return "", fmt.Errorf("invalid path")
	}
	abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	sep := string(os.PathSeparator)
	if abs != root && !strings.HasPrefix(abs, root+sep) {
		return "", fmt.Errorf("path escapes project root")
	}
	// If the path exists, resolve symlinks and re-check jail.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if resolved != root && !strings.HasPrefix(resolved, root+sep) {
			return "", fmt.Errorf("symlink escapes project root")
		}
		return resolved, nil
	}
	// Parent must stay in jail for create operations.
	parent := filepath.Dir(abs)
	if parent != root {
		if parentResolved, err := filepath.EvalSymlinks(parent); err == nil {
			if parentResolved != root && !strings.HasPrefix(parentResolved, root+sep) {
				return "", fmt.Errorf("parent escapes project root")
			}
		}
	}
	return abs, nil
}

func relFromRoot(root, abs string) string {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	if abs == root {
		return ""
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

func fileCrumbs(rel string) []projectFileCrumb {
	rel = strings.Trim(strings.TrimPrefix(rel, "/"), "/")
	if rel == "" {
		return nil
	}
	parts := strings.Split(rel, "/")
	out := make([]projectFileCrumb, 0, len(parts))
	acc := ""
	for _, p := range parts {
		if acc == "" {
			acc = p
		} else {
			acc += "/" + p
		}
		out = append(out, projectFileCrumb{Name: p, Path: acc})
	}
	return out
}

func isLikelyBinary(data []byte) bool {
	if !utf8.Valid(data) {
		return true
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func formatFileSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
}

func (h *handler) loadProjectForFiles(r *http.Request) (*storage.Project, error) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	return h.loadNodeProject(uint(id))
}

func (h *handler) ensureProjectRoot(p *storage.Project) (string, error) {
	root := h.projectFileRoot(p)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func (h *handler) filesScopeFromRequest(r *http.Request, p *storage.Project) (scope, container string) {
	scope = strings.TrimSpace(r.FormValue("scope"))
	if scope == "" {
		scope = strings.TrimSpace(r.URL.Query().Get("scope"))
	}
	container = strings.TrimSpace(r.FormValue("container"))
	if container == "" {
		container = strings.TrimSpace(r.URL.Query().Get("container"))
	}
	running := []string{}
	for _, c := range projectContainerCandidates(p) {
		if containerRunning(c) {
			running = append(running, c)
		}
	}
	if container != "" {
		ok := false
		for _, c := range running {
			if c == container {
				ok = true
				break
			}
		}
		if !ok {
			container = ""
		}
	}
	if container == "" && len(running) > 0 {
		container = running[0]
	}
	if scope != "host" && scope != "container" {
		if container != "" {
			scope = "container"
		} else {
			scope = "host"
		}
	}
	if scope == "container" && container == "" {
		scope = "host"
	}
	return scope, container
}

func (h *handler) redirectProjectFiles(w http.ResponseWriter, r *http.Request, id uint, path, flash string) {
	scope := strings.TrimSpace(r.FormValue("scope"))
	if scope == "" {
		scope = strings.TrimSpace(r.URL.Query().Get("scope"))
	}
	container := strings.TrimSpace(r.FormValue("container"))
	if container == "" {
		container = strings.TrimSpace(r.URL.Query().Get("container"))
	}
	u := fmt.Sprintf("/projects/%d?tab=files", id)
	if scope != "" {
		u += "&scope=" + urlQueryEscape(scope)
	}
	if container != "" {
		u += "&container=" + urlQueryEscape(container)
	}
	if path != "" {
		u += "&path=" + urlQueryEscape(path)
	}
	if flash != "" {
		u += "&flash=" + urlQueryEscape(flash)
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

func (h *handler) fillProjectFiles(data *pageData, p *storage.Project, rel, scope, container string) error {
	data.FileScope = scope
	data.FileContainer = container
	if scope == "container" && container != "" {
		wd := containerWorkDir(container)
		list, err := listContainerDir(container, wd, rel)
		if err != nil {
			return err
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].IsDir != list[j].IsDir {
				return list[i].IsDir
			}
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		})
		data.FileRoot = container + ":" + wd
		data.FilePath = strings.Trim(rel, "/")
		data.FileEntries = list
		data.FileCrumbs = fileCrumbs(rel)
		if data.FilePath != "" {
			parent := filepath.ToSlash(filepath.Dir(data.FilePath))
			if parent == "." {
				parent = ""
			}
			data.FileParent = parent
		}
		return nil
	}

	root, err := h.ensureProjectRoot(p)
	if err != nil {
		return err
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("not a directory")
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	var list []projectFileEntry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = strings.Trim(rel, "/") + "/" + name
		}
		sz := info.Size()
		list = append(list, projectFileEntry{
			Name:      name,
			RelPath:   childRel,
			Mode:      info.Mode().Perm().String(),
			IsDir:     e.IsDir(),
			Size:      sz,
			SizeHuman: formatFileSize(sz),
			Mod:       info.ModTime().Format("2006-01-02 15:04"),
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsDir != list[j].IsDir {
			return list[i].IsDir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	data.FileRoot = root
	data.FilePath = strings.Trim(rel, "/")
	data.FileEntries = list
	data.FileCrumbs = fileCrumbs(rel)
	if data.FilePath != "" {
		parent := filepath.ToSlash(filepath.Dir(data.FilePath))
		if parent == "." {
			parent = ""
		}
		data.FileParent = parent
	}
	return nil
}

func (h *handler) getNodeProjectFilesDownload(w http.ResponseWriter, r *http.Request) {
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	scope, container := h.filesScopeFromRequest(r, p)
	rel := r.URL.Query().Get("path")
	if scope == "container" {
		wd := containerWorkDir(container)
		abs, err := resolveContainerPath(wd, rel)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, err := dockerExecBytes(container, "cat", abs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, path.Base(abs)))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(data)
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(abs)))
	http.ServeFile(w, r, abs)
}

func (h *handler) postNodeProjectFilesUpload(w http.ResponseWriter, r *http.Request) {
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadBytes+1<<20)
	if err := r.ParseMultipartForm(maxFileUploadBytes); err != nil {
		h.redirectProjectFiles(w, r, p.ID, r.FormValue("path"), "upload too large or invalid")
		return
	}
	relDir := r.FormValue("path")
	scope, container := h.filesScopeFromRequest(r, p)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, "file required")
		return
	}
	defer file.Close()
	name := filepath.Base(hdr.Filename)
	if name == "" || name == "." || name == ".." {
		h.redirectProjectFiles(w, r, p.ID, relDir, "invalid filename")
		return
	}
	if scope == "container" {
		if err := uploadContainerFile(container, containerWorkDir(container), relDir, name, file); err != nil {
			h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
			return
		}
		h.redirectProjectFiles(w, r, p.ID, relDir, "Uploaded "+name)
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	dirAbs, err := resolveUnderRoot(root, relDir)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	dest := filepath.Join(dirAbs, name)
	dest, err = resolveUnderRoot(root, relFromRoot(root, dest))
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	n, copyErr := io.Copy(out, io.LimitReader(file, maxFileUploadBytes+1))
	_ = out.Close()
	if copyErr != nil || n > maxFileUploadBytes {
		_ = os.Remove(dest)
		h.redirectProjectFiles(w, r, p.ID, relDir, "upload exceeds 100 MiB limit")
		return
	}
	h.redirectProjectFiles(w, r, p.ID, relDir, "Uploaded "+name)
}

func (h *handler) postNodeProjectFilesMkdir(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	relDir := r.FormValue("path")
	name := strings.TrimSpace(r.FormValue("name"))
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." {
		h.redirectProjectFiles(w, r, p.ID, relDir, "folder name required")
		return
	}
	child := name
	if relDir != "" {
		child = strings.Trim(relDir, "/") + "/" + name
	}
	scope, container := h.filesScopeFromRequest(r, p)
	if scope == "container" {
		if err := mkdirContainer(container, containerWorkDir(container), child); err != nil {
			h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
			return
		}
		h.redirectProjectFiles(w, r, p.ID, relDir, "Folder created")
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, child)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	h.redirectProjectFiles(w, r, p.ID, relDir, "Folder created")
}

func (h *handler) postNodeProjectFilesCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	relDir := r.FormValue("path")
	name := filepath.Base(strings.TrimSpace(r.FormValue("name")))
	if name == "" || name == "." || name == ".." {
		h.redirectProjectFiles(w, r, p.ID, relDir, "file name required")
		return
	}
	child := name
	if relDir != "" {
		child = strings.Trim(relDir, "/") + "/" + name
	}
	scope, container := h.filesScopeFromRequest(r, p)
	if scope == "container" {
		if err := createContainerFile(container, containerWorkDir(container), child); err != nil {
			h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
			return
		}
		h.redirectProjectFiles(w, r, p.ID, relDir, "File created")
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, child)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, relDir, err.Error())
		return
	}
	_ = f.Close()
	h.redirectProjectFiles(w, r, p.ID, relDir, "File created")
}

func (h *handler) postNodeProjectFilesSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	content := r.FormValue("content")
	scope, container := h.filesScopeFromRequest(r, p)
	editURL := func(flash string) string {
		u := fmt.Sprintf("/projects/%d?tab=files&edit=1&path=%s&scope=%s", p.ID, urlQueryEscape(rel), urlQueryEscape(scope))
		if container != "" {
			u += "&container=" + urlQueryEscape(container)
		}
		if flash != "" {
			u += "&flash=" + urlQueryEscape(flash)
		}
		return u
	}
	if len(content) > maxFileEditBytes {
		http.Redirect(w, r, editURL("content exceeds 10 MiB"), http.StatusSeeOther)
		return
	}
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	if scope == "container" {
		if err := writeContainerFile(container, containerWorkDir(container), rel, []byte(content)); err != nil {
			http.Redirect(w, r, editURL(err.Error()), http.StatusSeeOther)
			return
		}
		h.redirectProjectFiles(w, r, p.ID, parent, "Saved "+filepath.Base(rel))
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		h.redirectProjectFiles(w, r, p.ID, parent, "file not found")
		return
	}
	if err := os.WriteFile(abs, []byte(content), st.Mode().Perm()); err != nil {
		http.Redirect(w, r, editURL(err.Error()), http.StatusSeeOther)
		return
	}
	h.redirectProjectFiles(w, r, p.ID, parent, "Saved "+filepath.Base(rel))
}

func (h *handler) postNodeProjectFilesRename(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	newName := filepath.Base(strings.TrimSpace(r.FormValue("name")))
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	if newName == "" || newName == "." || newName == ".." {
		h.redirectProjectFiles(w, r, p.ID, parent, "new name required")
		return
	}
	newRel := newName
	if parent != "" {
		newRel = parent + "/" + newName
	}
	scope, container := h.filesScopeFromRequest(r, p)
	if scope == "container" {
		if err := renameContainerPath(container, containerWorkDir(container), rel, newRel); err != nil {
			h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
			return
		}
		h.redirectProjectFiles(w, r, p.ID, parent, "Renamed")
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	oldAbs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	newAbs, err := resolveUnderRoot(root, newRel)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	h.redirectProjectFiles(w, r, p.ID, parent, "Renamed")
}

func (h *handler) postNodeProjectFilesDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	scope, container := h.filesScopeFromRequest(r, p)
	if scope == "container" {
		if err := deleteContainerPath(container, containerWorkDir(container), rel); err != nil {
			h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
			return
		}
		h.redirectProjectFiles(w, r, p.ID, parent, "Deleted")
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	if abs == root {
		h.redirectProjectFiles(w, r, p.ID, parent, "cannot delete project root")
		return
	}
	if err := os.RemoveAll(abs); err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	h.redirectProjectFiles(w, r, p.ID, parent, "Deleted")
}

func (h *handler) postNodeProjectFilesChmod(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p, err := h.loadProjectForFiles(r)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	modeStr := strings.TrimSpace(r.FormValue("mode"))
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	modeVal, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil || modeVal > 0o777 {
		h.redirectProjectFiles(w, r, p.ID, parent, "mode must be octal like 644 or 755")
		return
	}
	scope, container := h.filesScopeFromRequest(r, p)
	if scope == "container" {
		if err := chmodContainerPath(container, containerWorkDir(container), rel, modeStr); err != nil {
			h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
			return
		}
		h.redirectProjectFiles(w, r, p.ID, parent, "Permissions updated")
		return
	}
	root, err := h.ensureProjectRoot(p)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	if err := os.Chmod(abs, os.FileMode(modeVal)); err != nil {
		h.redirectProjectFiles(w, r, p.ID, parent, err.Error())
		return
	}
	h.redirectProjectFiles(w, r, p.ID, parent, "Permissions updated")
}

func loadFileForEdit(root, rel string) (body string, err error) {
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("cannot edit a directory")
	}
	if st.Size() > maxFileEditBytes {
		return "", fmt.Errorf("file larger than 10 MiB — download instead")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if isLikelyBinary(data) {
		return "", fmt.Errorf("binary file — download instead")
	}
	return string(data), nil
}
