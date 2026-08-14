package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/services/rustfs"
)

func (h *handler) rustfsClient() *rustfs.Client {
	cfg := rustfs.LoadConfig(h.opts.DB, h.localServerID())
	cfg = rustfs.EnrichFromContainer(cfg)
	return rustfs.NewClient(cfg)
}

func (h *handler) rustfsReady(ctx context.Context) (*rustfs.Client, rustfs.Config, error) {
	cfg := rustfs.LoadConfig(h.opts.DB, h.localServerID())
	cfg = rustfs.EnrichFromContainer(cfg)
	cli := rustfs.NewClient(cfg)
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := cli.Ping(ctx); err != nil {
		return nil, cfg, err
	}
	return cli, cfg, nil
}

func (h *handler) getNodeStorage(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	data := h.basePage(sess, "Storage")
	data.ActiveNav = "storage"
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	cfg := rustfs.LoadConfig(h.opts.DB, h.localServerID())
	cfg = rustfs.EnrichFromContainer(cfg)
	data.StorageEndpoint = cfg.EndpointURL()
	data.StorageConsoleURL = cfg.ConsoleURL()

	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		data.StorageReady = false
		if data.Flash == "" {
			data.Flash = "RustFS offline — enable it under Services first"
		}
		h.render(w, "node_storage", data)
		return
	}
	data.StorageReady = true

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	buckets, err := cli.ListBuckets(ctx)
	if err != nil {
		data.Flash = "List buckets failed: " + err.Error()
	} else {
		data.StorageBuckets = buckets
		for _, b := range buckets {
			data.StorageTotalObjects += b.ObjectCount
			data.StorageTotalBytes += b.TotalBytes
		}
		data.StorageTotalHuman = nodemetrics.FormatBytes(uint64(data.StorageTotalBytes))
	}
	h.render(w, "node_storage", data)
}

func (h *handler) getNodeStorageBucket(w http.ResponseWriter, r *http.Request) {
	sess := sessionFromCtx(r.Context())
	bucket := strings.TrimSpace(r.PathValue("bucket"))
	prefix := strings.TrimSpace(r.URL.Query().Get("prefix"))

	data := h.basePage(sess, "Storage · "+bucket)
	data.ActiveNav = "storage"
	data.StorageBucket = bucket
	data.StoragePrefix = strings.Trim(prefix, "/")
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash = flash
	}

	cli, cfg, err := h.rustfsReady(r.Context())
	if err != nil {
		data.Flash = "RustFS offline: " + err.Error()
		h.render(w, "node_storage_bucket", data)
		return
	}
	data.StorageReady = true
	data.StorageEndpoint = cfg.EndpointURL()

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	entries, err := cli.ListObjects(ctx, bucket, prefix)
	if err != nil {
		data.Flash = "List objects failed: " + err.Error()
	} else {
		data.StorageObjects = entries
	}
	data.StorageCrumbs = storageCrumbs(prefix)
	if p := strings.Trim(prefix, "/"); p != "" {
		parent := path.Dir(p)
		if parent == "." {
			parent = ""
		}
		data.StorageParent = parent
	}
	h.render(w, "node_storage_bucket", data)
}

func storageCrumbs(prefix string) []projectFileCrumb {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return nil
	}
	parts := strings.Split(prefix, "/")
	out := make([]projectFileCrumb, 0, len(parts))
	acc := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if acc == "" {
			acc = p
		} else {
			acc += "/" + p
		}
		out = append(out, projectFileCrumb{Name: p, Path: acc})
	}
	return out
}

func (h *handler) postNodeStorageBucketCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		http.Redirect(w, r, "/storage?flash="+urlQueryEscape("RustFS offline"), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := cli.CreateBucket(ctx, name); err != nil {
		http.Redirect(w, r, "/storage?flash="+urlQueryEscape("Create failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/storage?flash="+urlQueryEscape("Bucket "+name+" created"), http.StatusSeeOther)
}

func (h *handler) postNodeStorageBucketDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSpace(r.PathValue("bucket"))
	}
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		http.Redirect(w, r, "/storage?flash="+urlQueryEscape("RustFS offline"), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	_ = cli.EmptyBucket(ctx, name)
	if err := cli.DeleteBucket(ctx, name); err != nil {
		http.Redirect(w, r, "/storage?flash="+urlQueryEscape("Delete failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/storage?flash="+urlQueryEscape("Bucket "+name+" deleted"), http.StatusSeeOther)
}

func redirectStorage(w http.ResponseWriter, r *http.Request, bucket, prefix, flash string) {
	u := fmt.Sprintf("/storage/buckets/%s", url.PathEscape(bucket))
	q := ""
	if prefix != "" {
		q += "prefix=" + urlQueryEscape(prefix)
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

func (h *handler) postNodeStorageUpload(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimSpace(r.PathValue("bucket"))
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		redirectStorage(w, r, bucket, "", "upload parse failed")
		return
	}
	prefix := strings.Trim(r.FormValue("prefix"), "/")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		redirectStorage(w, r, bucket, prefix, "file required")
		return
	}
	defer file.Close()

	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		redirectStorage(w, r, bucket, prefix, "RustFS offline")
		return
	}
	key := hdr.Filename
	if prefix != "" {
		key = prefix + "/" + hdr.Filename
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	ct := hdr.Header.Get("Content-Type")
	if err := cli.PutObject(ctx, bucket, key, file, hdr.Size, ct); err != nil {
		redirectStorage(w, r, bucket, prefix, "Upload failed: "+err.Error())
		return
	}
	redirectStorage(w, r, bucket, prefix, "Uploaded "+hdr.Filename)
}

func (h *handler) postNodeStorageMkdir(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	bucket := strings.TrimSpace(r.PathValue("bucket"))
	prefix := strings.Trim(r.FormValue("prefix"), "/")
	name := strings.Trim(strings.TrimSpace(r.FormValue("name")), "/")
	if name == "" || strings.Contains(name, "/") {
		redirectStorage(w, r, bucket, prefix, "folder name required (no slashes)")
		return
	}
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		redirectStorage(w, r, bucket, prefix, "RustFS offline")
		return
	}
	full := name
	if prefix != "" {
		full = prefix + "/" + name
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := cli.Mkdir(ctx, bucket, full); err != nil {
		redirectStorage(w, r, bucket, prefix, "Mkdir failed: "+err.Error())
		return
	}
	redirectStorage(w, r, bucket, prefix, "Folder "+name+" created")
}

func (h *handler) postNodeStorageDelete(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	bucket := strings.TrimSpace(r.PathValue("bucket"))
	prefix := strings.Trim(r.FormValue("prefix"), "/")
	key := strings.TrimSpace(r.FormValue("key"))
	isDir := r.FormValue("isdir") == "1"
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		redirectStorage(w, r, bucket, prefix, "RustFS offline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if isDir {
		err = cli.DeletePrefix(ctx, bucket, key)
	} else {
		err = cli.DeleteObject(ctx, bucket, key)
	}
	if err != nil {
		redirectStorage(w, r, bucket, prefix, "Delete failed: "+err.Error())
		return
	}
	redirectStorage(w, r, bucket, prefix, "Deleted")
}

func (h *handler) postNodeStorageRename(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	bucket := strings.TrimSpace(r.PathValue("bucket"))
	prefix := strings.Trim(r.FormValue("prefix"), "/")
	key := strings.TrimSpace(r.FormValue("key"))
	name := strings.Trim(strings.TrimSpace(r.FormValue("name")), "/")
	if name == "" || strings.Contains(name, "/") {
		redirectStorage(w, r, bucket, prefix, "new name required (no slashes)")
		return
	}
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		redirectStorage(w, r, bucket, prefix, "RustFS offline")
		return
	}
	dir := path.Dir(strings.TrimSuffix(key, "/"))
	if dir == "." {
		dir = ""
	}
	dest := name
	if dir != "" {
		dest = dir + "/" + name
	}
	if strings.HasSuffix(key, "/") {
		redirectStorage(w, r, bucket, prefix, "rename folder not supported — use object rename")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := cli.RenameObject(ctx, bucket, key, dest); err != nil {
		redirectStorage(w, r, bucket, prefix, "Rename failed: "+err.Error())
		return
	}
	redirectStorage(w, r, bucket, prefix, "Renamed")
}

func (h *handler) getNodeStorageDownload(w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimSpace(r.PathValue("bucket"))
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	cli, _, err := h.rustfsReady(r.Context())
	if err != nil {
		http.Error(w, "RustFS offline", http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	body, ct, _, err := cli.GetObject(ctx, bucket, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer body.Close()
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, path.Base(key)))
	_, _ = io.Copy(w, body)
}
