package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lyracorp/xmanager/internal/backup"
	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/storage"
)

type backupTargetView struct {
	Type string
	Name string
	Key  string // type:name
}

type backupHistoryView struct {
	storage.Backup
	SizeHuman string
}

func (h *handler) getNodeBackup(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	sid := h.localServerID()
	exec := h.localExec()
	data := h.basePage(sess, "Backup")
	data.ActiveNav = "backup"
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	for _, t := range backup.ListDumpable(exec) {
		data.BackupTargets = append(data.BackupTargets, backupTargetView{
			Type: string(t.Type),
			Name: t.Name,
			Key:  string(t.Type) + ":" + t.Name,
		})
	}

	var dests []storage.BackupDestination
	h.opts.DB.Where("server_id = ?", sid).Order("name asc").Find(&dests)
	data.BackupDestinations = dests

	var channels []storage.AlertChannel
	h.opts.DB.Where("type = ? AND enabled = ?", "telegram", true).Find(&channels)
	data.BackupTelegramChannels = channels

	var hist []storage.Backup
	h.opts.DB.Where("server_id = ?", sid).Order("backed_at desc").Limit(100).Find(&hist)
	for _, b := range hist {
		data.BackupHistory = append(data.BackupHistory, backupHistoryView{
			Backup:    b,
			SizeHuman: backup.FormatSize(b.Size),
		})
	}

	h.render(w, "node_backup", data)
}

func (h *handler) postNodeBackupRun(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid := h.localServerID()
	exec := h.localExec()
	all := r.FormValue("all") == "1"
	keys := r.Form["db"]
	destIDs := r.Form["dest"]

	var targets []backup.Target
	if all {
		targets = backup.ListDumpable(exec)
	} else {
		for _, k := range keys {
			parts := strings.SplitN(k, ":", 2)
			if len(parts) != 2 {
				continue
			}
			targets = append(targets, backup.Target{Type: dbmanager.DBType(parts[0]), Name: parts[1]})
		}
	}
	if len(targets) == 0 {
		http.Redirect(w, r, "/backup?flash="+urlQueryEscape("select at least one database"), http.StatusSeeOther)
		return
	}

	var dests []storage.BackupDestination
	for _, idStr := range destIDs {
		id, _ := strconv.ParseUint(idStr, 10, 64)
		if id == 0 {
			continue
		}
		var d storage.BackupDestination
		if err := h.opts.DB.Where("server_id = ? AND id = ? AND enabled = ?", sid, id, true).First(&d).Error; err == nil {
			dests = append(dests, d)
		}
	}

	okN, failN := 0, 0
	var notes []string
	for _, t := range targets {
		res := backup.RunOne(exec, t.Type, t.Name, backup.DefaultDir)
		destLabels := []string{"local"}
		var deliverErrs []string
		if res.Err == nil {
			for _, d := range dests {
				if err := backup.Deliver(exec, h.opts.DB, d, res.Path, res.Filename); err != nil {
					deliverErrs = append(deliverErrs, d.Name+": "+err.Error())
				} else {
					destLabels = append(destLabels, d.Type+":"+d.Name)
				}
			}
		}
		destStr := strings.Join(destLabels, ",")
		if len(deliverErrs) > 0 && res.Err == nil {
			res.Err = fmt.Errorf("delivered with errors: %s", strings.Join(deliverErrs, "; "))
		}
		backup.Record(h.opts.DB, sid, res, destStr)
		if res.Err != nil {
			failN++
			notes = append(notes, fmt.Sprintf("%s/%s failed", t.Type, t.Name))
		} else {
			okN++
		}
	}
	flash := fmt.Sprintf("Backup finished: %d ok, %d failed", okN, failN)
	if len(notes) > 0 && len(notes) <= 3 {
		flash += " — " + strings.Join(notes, "; ")
	}
	http.Redirect(w, r, "/backup?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}

func (h *handler) postNodeBackupDestination(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	sid := h.localServerID()
	name := strings.TrimSpace(r.FormValue("name"))
	typ := strings.ToLower(strings.TrimSpace(r.FormValue("type")))
	if name == "" || (typ != "ftp" && typ != "scp" && typ != "telegram") {
		http.Redirect(w, r, "/backup?flash="+urlQueryEscape("name and type required"), http.StatusSeeOther)
		return
	}
	cfg := backup.DestConfig{
		Host:     strings.TrimSpace(r.FormValue("host")),
		Port:     strings.TrimSpace(r.FormValue("port")),
		User:     strings.TrimSpace(r.FormValue("user")),
		Password: r.FormValue("password"),
		Path:     strings.TrimSpace(r.FormValue("path")),
		KeyPath:  strings.TrimSpace(r.FormValue("key_path")),
		BotToken: strings.TrimSpace(r.FormValue("bot_token")),
		ChatID:   strings.TrimSpace(r.FormValue("chat_id")),
	}
	if ch := r.FormValue("channel_id"); ch != "" {
		id, _ := strconv.ParseUint(ch, 10, 64)
		cfg.ChannelID = uint(id)
	}
	d := storage.BackupDestination{
		ServerID:   sid,
		Name:       name,
		Type:       typ,
		ConfigJSON: cfg.JSON(),
		Enabled:    true,
	}
	if err := h.opts.DB.Create(&d).Error; err != nil {
		http.Redirect(w, r, "/backup?flash="+urlQueryEscape("save failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/backup?flash="+urlQueryEscape("Destination "+name+" saved"), http.StatusSeeOther)
}

func (h *handler) postNodeBackupDestinationDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	h.opts.DB.Where("server_id = ? AND id = ?", h.localServerID(), id).Delete(&storage.BackupDestination{})
	http.Redirect(w, r, "/backup?flash="+urlQueryEscape("Destination deleted"), http.StatusSeeOther)
}

func (h *handler) getNodeBackupDownload(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var b storage.Backup
	if err := h.opts.DB.Where("server_id = ? AND id = ?", h.localServerID(), id).First(&b).Error; err != nil {
		http.NotFound(w, r)
		return
	}
	path, err := backup.SafeBackupPath(b.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "file not found on disk", http.StatusNotFound)
		return
	}
	defer f.Close()
	name := b.Filename
	if name == "" {
		name = filepath.Base(path)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	_, _ = io.Copy(w, f)
}

func (h *handler) postNodeBackupDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var b storage.Backup
	if err := h.opts.DB.Where("server_id = ? AND id = ?", h.localServerID(), id).First(&b).Error; err == nil {
		if path, err := backup.SafeBackupPath(b.Path); err == nil {
			_ = os.Remove(path)
		}
		h.opts.DB.Delete(&b)
	}
	http.Redirect(w, r, "/backup?flash="+urlQueryEscape("Backup deleted"), http.StatusSeeOther)
}

func (h *handler) postNodeBackupResend(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	id, _ := strconv.ParseUint(r.PathValue("id"), 10, 64)
	sid := h.localServerID()
	var b storage.Backup
	if err := h.opts.DB.Where("server_id = ? AND id = ?", sid, id).First(&b).Error; err != nil {
		http.Redirect(w, r, "/backup?flash="+urlQueryEscape("not found"), http.StatusSeeOther)
		return
	}
	path, err := backup.SafeBackupPath(b.Path)
	if err != nil {
		http.Redirect(w, r, "/backup?flash="+urlQueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	filename := b.Filename
	if filename == "" {
		filename = filepath.Base(path)
	}
	exec := h.localExec()
	var errs []string
	for _, idStr := range r.Form["dest"] {
		did, _ := strconv.ParseUint(idStr, 10, 64)
		var d storage.BackupDestination
		if err := h.opts.DB.Where("server_id = ? AND id = ?", sid, did).First(&d).Error; err != nil {
			continue
		}
		if err := backup.Deliver(exec, h.opts.DB, d, path, filename); err != nil {
			errs = append(errs, d.Name+": "+err.Error())
		}
	}
	flash := "Resent"
	if len(errs) > 0 {
		flash = "Resend errors: " + strings.Join(errs, "; ")
	}
	http.Redirect(w, r, "/backup?flash="+urlQueryEscape(flash), http.StatusSeeOther)
}
