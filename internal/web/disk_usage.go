package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/nodemetrics"
	"github.com/lyracorp/xmanager/internal/ssh"
)

// diskUsageView is the home disk-breakdown model.
type diskUsageView struct {
	DiskUsedGB     float64
	DiskTotalGB    float64
	DiskPct        float64
	RootBytes      uint64
	Projects       uint64
	Images         uint64
	Volumes        uint64
	BuildCache     uint64
	Databases      uint64
	Storage        uint64
	ProjectsHuman  string
	ImagesHuman    string
	VolumesHuman   string
	BuildHuman     string
	DatabasesHuman string
	StorageHuman   string
	Rows           []diskUsageRow
	SampledAt      time.Time
	Error          string
}

type diskUsageRow struct {
	Label  string
	Bytes  uint64
	Human  string
	Pct    float64 // of root disk used when known
	BarPct float64 // width for UI bar (0-100 relative to largest category)
}

type diskUsageCache struct {
	mu  sync.Mutex
	at  time.Time
	view diskUsageView
	ttl time.Duration
}

func (h *handler) getDiskUsageCached() diskUsageView {
	if h.diskCache == nil {
		h.diskCache = &diskUsageCache{ttl: 60 * time.Second}
	}
	h.diskCache.mu.Lock()
	defer h.diskCache.mu.Unlock()
	if time.Since(h.diskCache.at) < h.diskCache.ttl && !h.diskCache.at.IsZero() {
		return h.diskCache.view
	}
	view := h.collectDiskUsage()
	h.diskCache.view = view
	h.diskCache.at = time.Now()
	return view
}

func (h *handler) collectDiskUsage() diskUsageView {
	var usedGB, totalGB, pct float64
	if h.node != nil {
		s := h.node.Latest()
		usedGB, totalGB, pct = s.DiskUsedGB, s.DiskTotalGB, s.DiskPct
	} else {
		s := nodemetrics.Sample()
		usedGB, totalGB, pct = s.DiskUsedGB, s.DiskTotalGB, s.DiskPct
	}

	v := diskUsageView{
		DiskUsedGB:  usedGB,
		DiskTotalGB: totalGB,
		DiskPct:     pct,
		RootBytes:   uint64(totalGB * 1024 * 1024 * 1024),
		SampledAt:   time.Now(),
	}

	exec := h.localExec()
	v.Projects = sumDuBytes(exec,
		"/opt/xmanager/projects",
		"/opt/xmanager/functions",
	)

	if rows, err := docker.NewManager(exec).SystemDF(); err == nil {
		v.Images = docker.DFBytesByType(rows, "Images")
		v.Volumes = docker.DFBytesByType(rows, "Volumes")
		v.BuildCache = docker.DFBytesByType(rows, "Build Cache")
	}

	v.Databases = sumDatabaseBytes(exec)
	v.Storage = h.sumRustFSBytes()

	v.ProjectsHuman = nodemetrics.FormatBytes(v.Projects)
	v.ImagesHuman = nodemetrics.FormatBytes(v.Images)
	v.VolumesHuman = nodemetrics.FormatBytes(v.Volumes)
	v.BuildHuman = nodemetrics.FormatBytes(v.BuildCache)
	v.DatabasesHuman = nodemetrics.FormatBytes(v.Databases)
	v.StorageHuman = nodemetrics.FormatBytes(v.Storage)

	cats := []struct {
		label string
		n     uint64
	}{
		{"Projects", v.Projects},
		{"Docker images", v.Images},
		{"Docker volumes", v.Volumes},
		{"Docker build cache", v.BuildCache},
		{"Databases", v.Databases},
		{"Storage (RustFS)", v.Storage},
	}
	var max uint64
	for _, c := range cats {
		if c.n > max {
			max = c.n
		}
	}
	rootUsed := uint64(usedGB * 1024 * 1024 * 1024)
	for _, c := range cats {
		row := diskUsageRow{
			Label: c.label,
			Bytes: c.n,
			Human: nodemetrics.FormatBytes(c.n),
		}
		if rootUsed > 0 {
			row.Pct = float64(c.n) / float64(rootUsed) * 100
		}
		if max > 0 {
			row.BarPct = float64(c.n) / float64(max) * 100
		}
		v.Rows = append(v.Rows, row)
	}
	return v
}

func sumDuBytes(exec *ssh.Executor, paths ...string) uint64 {
	if exec == nil {
		return 0
	}
	var total uint64
	for _, p := range paths {
		out := exec.RunQuiet(fmt.Sprintf("du -sb %s 2>/dev/null | awk '{print $1}'", shellQuotePath(p)))
		out = strings.TrimSpace(out)
		if out == "" {
			continue
		}
		n, _ := strconv.ParseUint(out, 10, 64)
		total += n
	}
	return total
}

func shellQuotePath(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

func sumDatabaseBytes(exec *ssh.Executor) uint64 {
	if exec == nil {
		return 0
	}
	var total uint64
	for _, t := range []dbmanager.DBType{
		dbmanager.PostgreSQL, dbmanager.MySQL, dbmanager.MariaDB, dbmanager.MongoDB,
	} {
		mgr := dbmanager.NewManager(t, exec)
		if mgr == nil || !mgr.IsAvailable() {
			continue
		}
		dbs, err := mgr.ListDatabases()
		if err != nil {
			continue
		}
		for _, db := range dbs {
			n := nodemetrics.ParseHumanSize(db.Size)
			if n == 0 && (db.Size == "-" || db.Size == "") {
				if info, err := dbmanager.DatabaseInfo(t, exec, db.Name); err == nil {
					n = nodemetrics.ParseHumanSize(info.Size)
				}
			}
			total += n
		}
	}
	return total
}

func (h *handler) sumRustFSBytes() uint64 {
	cli, _, err := h.rustfsReady(context.Background())
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	buckets, err := cli.ListBuckets(ctx)
	if err != nil {
		return 0
	}
	var total uint64
	for _, b := range buckets {
		if b.TotalBytes > 0 {
			total += uint64(b.TotalBytes)
		}
	}
	return total
}

func (h *handler) countRustFSBuckets() int {
	cli, _, err := h.rustfsReady(context.Background())
	if err != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	buckets, err := cli.ListBuckets(ctx)
	if err != nil {
		return 0
	}
	return len(buckets)
}
