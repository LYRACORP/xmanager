package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/storage"
)

func (h *handler) redirectFTPFiles(w http.ResponseWriter, r *http.Request, id uint, rel, flash string) {
	u := fmt.Sprintf("/ftp/users/%d/files", id)
	q := ""
	if rel != "" {
		q += "path=" + urlQueryEscape(rel)
	}
	if flash != "" {
		if q != "" {
			q += "&"
		}
		q += "flash=" + urlQueryEscape(flash)
	}
	if q != "" {
		u += "?" + q
	}
	http.Redirect(w, r, u, http.StatusSeeOther)
}

func (h *handler) ensureFTPHome(u *storage.FTPUser) (string, error) {
	root := u.Home
	if root == "" {
		return "", fmt.Errorf("no home directory")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

func (h *handler) fillFTPFiles(data *pageData, u *storage.FTPUser, rel string) error {
	root, err := h.ensureFTPHome(u)
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
	data.FTPUser = u
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

func (h *handler) getNodeFTPFiles(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp?flash="+urlQueryEscape("user not found"), http.StatusSeeOther)
		return
	}
	rel := r.URL.Query().Get("path")
	data := h.basePage(sess, "FTP · "+u.Username)
	data.ActiveNav = "ftp"
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}
	if r.URL.Query().Get("edit") == "1" {
		root, err := h.ensureFTPHome(u)
		if err != nil {
			data.Flash = err.Error()
		} else {
			body, err := loadFileForEdit(root, rel)
			if err != nil {
				data.Flash = err.Error()
			} else {
				data.FileEditPath = strings.Trim(rel, "/")
				data.FileEditBody = body
				parent := filepath.ToSlash(filepath.Dir(data.FileEditPath))
				if parent == "." {
					parent = ""
				}
				data.FilePath = parent
			}
		}
		data.FTPUser = u
		data.FileRoot = u.Home
		data.FileCrumbs = fileCrumbs(data.FileEditPath)
	} else if err := h.fillFTPFiles(&data, u, rel); err != nil {
		data.Flash = err.Error()
		data.FTPUser = u
	}
	h.render(w, "node_ftp_files", data)
}

func (h *handler) getNodeFTPFilesDownload(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	abs, err := resolveUnderRoot(root, r.URL.Query().Get("path"))
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

func (h *handler) postNodeFTPFilesUpload(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadBytes+1<<20)
	if err := r.ParseMultipartForm(maxFileUploadBytes); err != nil {
		h.redirectFTPFiles(w, r, u.ID, r.FormValue("path"), "upload too large or invalid")
		return
	}
	relDir := r.FormValue("path")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, "file required")
		return
	}
	defer file.Close()
	name := filepath.Base(hdr.Filename)
	if name == "" || name == "." || name == ".." {
		h.redirectFTPFiles(w, r, u.ID, relDir, "invalid filename")
		return
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	dirAbs, err := resolveUnderRoot(root, relDir)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	dest := filepath.Join(dirAbs, name)
	dest, err = resolveUnderRoot(root, relFromRoot(root, dest))
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	n, copyErr := io.Copy(out, io.LimitReader(file, maxFileUploadBytes+1))
	_ = out.Close()
	if copyErr != nil || n > maxFileUploadBytes {
		_ = os.Remove(dest)
		h.redirectFTPFiles(w, r, u.ID, relDir, "upload exceeds 100 MiB limit")
		return
	}
	h.redirectFTPFiles(w, r, u.ID, relDir, "Uploaded "+name)
}

func (h *handler) postNodeFTPFilesMkdir(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
		return
	}
	relDir := r.FormValue("path")
	name := filepath.Base(strings.TrimSpace(r.FormValue("name")))
	if name == "" || name == "." || name == ".." {
		h.redirectFTPFiles(w, r, u.ID, relDir, "folder name required")
		return
	}
	child := name
	if relDir != "" {
		child = strings.Trim(relDir, "/") + "/" + name
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, child)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	h.redirectFTPFiles(w, r, u.ID, relDir, "Folder created")
}

func (h *handler) postNodeFTPFilesCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
		return
	}
	relDir := r.FormValue("path")
	name := filepath.Base(strings.TrimSpace(r.FormValue("name")))
	if name == "" || name == "." || name == ".." {
		h.redirectFTPFiles(w, r, u.ID, relDir, "file name required")
		return
	}
	child := name
	if relDir != "" {
		child = strings.Trim(relDir, "/") + "/" + name
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, child)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, relDir, err.Error())
		return
	}
	_ = f.Close()
	h.redirectFTPFiles(w, r, u.ID, relDir, "File created")
}

func (h *handler) postNodeFTPFilesSave(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	content := r.FormValue("content")
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	if len(content) > maxFileEditBytes {
		h.redirectFTPFiles(w, r, u.ID, parent, "content exceeds 10 MiB")
		return
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		h.redirectFTPFiles(w, r, u.ID, parent, "file not found")
		return
	}
	if err := os.WriteFile(abs, []byte(content), st.Mode().Perm()); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/ftp/users/%d/files?edit=1&path=%s&flash=%s", u.ID, urlQueryEscape(rel), urlQueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	h.redirectFTPFiles(w, r, u.ID, parent, "Saved "+filepath.Base(rel))
}

func (h *handler) postNodeFTPFilesRename(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	newName := filepath.Base(strings.TrimSpace(r.FormValue("name")))
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	if newName == "" || newName == "." || newName == ".." {
		h.redirectFTPFiles(w, r, u.ID, parent, "new name required")
		return
	}
	newRel := newName
	if parent != "" {
		newRel = parent + "/" + newName
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	oldAbs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	newAbs, err := resolveUnderRoot(root, newRel)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	h.redirectFTPFiles(w, r, u.ID, parent, "Renamed")
}

func (h *handler) postNodeFTPFilesDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
		return
	}
	rel := r.FormValue("path")
	parent := filepath.ToSlash(filepath.Dir(rel))
	if parent == "." {
		parent = ""
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	if abs == root {
		h.redirectFTPFiles(w, r, u.ID, parent, "cannot delete FTP home")
		return
	}
	if err := os.RemoveAll(abs); err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	h.redirectFTPFiles(w, r, u.ID, parent, "Deleted")
}

func (h *handler) postNodeFTPFilesChmod(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	u, err := h.loadOwnedFTPUser(r, uint(id))
	if err != nil {
		http.Redirect(w, r, "/ftp", http.StatusSeeOther)
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
		h.redirectFTPFiles(w, r, u.ID, parent, "mode must be octal like 644 or 755")
		return
	}
	root, err := h.ensureFTPHome(u)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	abs, err := resolveUnderRoot(root, rel)
	if err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	if err := os.Chmod(abs, os.FileMode(modeVal)); err != nil {
		h.redirectFTPFiles(w, r, u.ID, parent, err.Error())
		return
	}
	h.redirectFTPFiles(w, r, u.ID, parent, "Permissions updated")
}
