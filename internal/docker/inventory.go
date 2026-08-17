package docker

import (
	"fmt"
	"sort"
	"strings"
)

// LoginInfo is the registry login state on the Docker host.
type LoginInfo struct {
	Username   string   // Docker Hub username; empty if not logged in there
	Registries []string // registries with stored credentials (no secrets)
	CredsStore string
}

func (l LoginInfo) LoggedIn() bool {
	return strings.TrimSpace(l.Username) != "" || len(l.Registries) > 0
}

// AccountLine is a one-line summary for TUI/web headers.
func (l LoginInfo) AccountLine() string {
	regs := uniqueNonEmpty(l.Registries)
	user := strings.TrimSpace(l.Username)
	switch {
	case user != "" && len(regs) > 0:
		return user + " @ " + strings.Join(regs, ", ")
	case user != "":
		return user + " @ docker.io"
	case len(regs) > 0:
		return strings.Join(regs, ", ")
	default:
		return "not logged in"
	}
}

// Volume is a docker volume with optional disk usage.
type Volume struct {
	Name      string
	Driver    string
	Scope     string
	Links     int
	SizeHuman string
	SizeBytes uint64
}

// Network is a docker network.
type Network struct {
	ID         string
	Name       string
	Driver     string
	Scope      string
	Internal   bool
	Containers int
	Subnet     string
}

// BuildCache is one build-cache record from `docker system df -v`.
type BuildCache struct {
	ID          string
	Type        string
	SizeHuman   string
	SizeBytes   uint64
	Usage       int
	Shared      bool
	Reclaimable bool
}

// HostSnapshot is a full Docker inventory for the web panel.
type HostSnapshot struct {
	Login    LoginInfo
	DF       []SystemDFRow
	Images   []Image
	Volumes  []Volume
	Networks []Network
	Cache    []BuildCache
}

func (s HostSnapshot) DFHuman(typeName string) string {
	if row, ok := DFRowByType(s.DF, typeName); ok && row.SizeHuman != "" {
		return row.SizeHuman
	}
	return "—"
}

func (s HostSnapshot) DFCount(typeName string) int {
	if row, ok := DFRowByType(s.DF, typeName); ok {
		return row.TotalCount
	}
	return 0
}

func (m *Manager) LoginInfo() (LoginInfo, error) {
	out, err := m.run(loginProbeCmd)
	if err != nil {
		// Username probe can fail when docker is down; still try to parse stdout.
		if out == "" {
			return LoginInfo{}, err
		}
	}
	return ParseLoginProbe(out), nil
}

func (m *Manager) ListVolumes() ([]Volume, error) {
	meta, err := m.listVolumeMeta()
	verbose, verr := m.verboseDF()
	if err != nil && verr != nil {
		return nil, err
	}
	vols := mergeVolumes(meta, verbose.Volumes)
	sort.Slice(vols, func(i, j int) bool { return vols[i].SizeBytes > vols[j].SizeBytes })
	return vols, nil
}

func (m *Manager) ListNetworks() ([]Network, error) {
	out, err := m.run(`ids=$(docker network ls -q 2>/dev/null); if [ -n "$ids" ]; then docker network inspect $ids --format '{{.Name}}	{{.Id}}	{{.Driver}}	{{.Scope}}	{{if .Internal}}yes{{else}}no{{end}}	{{len .Containers}}	{{range .IPAM.Config}}{{.Subnet}} {{end}}'; fi`)
	if err != nil {
		return nil, err
	}
	return ParseNetworkInspect(out), nil
}

func (m *Manager) ListBuildCache() ([]BuildCache, error) {
	verbose, err := m.verboseDF()
	if err != nil {
		return nil, err
	}
	cache := verbose.Cache
	if len(cache) == 0 {
		if out, berr := m.run(`docker buildx du 2>/dev/null`); berr == nil {
			cache = ParseBuildxDU(out)
		}
	}
	sort.Slice(cache, func(i, j int) bool { return cache[i].SizeBytes > cache[j].SizeBytes })
	if len(cache) > 500 {
		cache = cache[:500]
	}
	return cache, nil
}

func (m *Manager) PruneVolumes() (string, error) {
	return m.run("docker volume prune -f")
}

func (m *Manager) PruneNetworks() (string, error) {
	return m.run("docker network prune -f")
}

func (m *Manager) PruneBuildCache() (string, error) {
	return m.run("docker builder prune -f")
}

// Snapshot gathers login, disk totals, images, volumes, networks, and build cache.
func (m *Manager) Snapshot() HostSnapshot {
	var snap HostSnapshot
	snap.Login, _ = m.LoginInfo()
	snap.DF, _ = m.SystemDF()
	snap.Images, _ = m.ListImages()
	snap.Networks, _ = m.ListNetworks()

	meta, _ := m.listVolumeMeta()
	verbose, _ := m.verboseDF()
	snap.Volumes = mergeVolumes(meta, verbose.Volumes)
	sort.Slice(snap.Volumes, func(i, j int) bool { return snap.Volumes[i].SizeBytes > snap.Volumes[j].SizeBytes })
	snap.Cache = verbose.Cache
	if len(snap.Cache) == 0 {
		if out, err := m.run(`docker buildx du 2>/dev/null`); err == nil {
			snap.Cache = ParseBuildxDU(out)
		}
	}
	sort.Slice(snap.Cache, func(i, j int) bool { return snap.Cache[i].SizeBytes > snap.Cache[j].SizeBytes })
	if len(snap.Cache) > 500 {
		snap.Cache = snap.Cache[:500]
	}
	return snap
}

func (m *Manager) listVolumeMeta() ([]Volume, error) {
	out, err := m.run(`docker volume ls --format '{{json .}}'`)
	if err != nil {
		return nil, err
	}
	return ParseVolumeLSJSON(out)
}

func (m *Manager) verboseDF() (verboseDF, error) {
	out, err := m.run("docker system df -v")
	if err != nil {
		return verboseDF{}, err
	}
	return ParseVerboseDF(out), nil
}

func mergeVolumes(meta, sized []Volume) []Volume {
	byName := make(map[string]Volume, len(meta)+len(sized))
	for _, v := range meta {
		if v.Name == "" {
			continue
		}
		byName[v.Name] = v
	}
	for _, v := range sized {
		if v.Name == "" {
			continue
		}
		cur := byName[v.Name]
		cur.Name = v.Name
		cur.Links = v.Links
		cur.SizeHuman = v.SizeHuman
		cur.SizeBytes = v.SizeBytes
		if cur.Driver == "" {
			cur.Driver = v.Driver
		}
		if cur.Scope == "" {
			cur.Scope = v.Scope
		}
		byName[v.Name] = cur
	}
	out := make([]Volume, 0, len(byName))
	for _, v := range byName {
		if v.SizeHuman == "" && v.SizeBytes > 0 {
			v.SizeHuman = FormatSize(v.SizeBytes)
		}
		if v.SizeHuman == "" {
			v.SizeHuman = "—"
		}
		out = append(out, v)
	}
	return out
}

func uniqueNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func shQuote(s string) string {
	if s == "" {
		return "''"
	}
	return `'` + strings.ReplaceAll(s, `'`, `'"'"'`) + `'`
}

// loginProbeCmd prints Hub username, then auth registry keys (never secret values).
var loginProbeCmd = fmt.Sprintf(`docker info --format '{{.Username}}' 2>/dev/null; echo '---XM---'; python3 -c %s 2>/dev/null || true`, shQuote(loginAuthPy))

const loginAuthPy = `import json,os,sys
p=os.path.expanduser("~/.docker/config.json")
if not os.path.isfile(p):
    sys.exit(0)
d=json.load(open(p))
for k in (d.get("auths") or {}):
    print("auth\t"+str(k).replace("\t"," ").replace("\n"," "))
for k in (d.get("credHelpers") or {}):
    print("helper\t"+str(k).replace("\t"," ").replace("\n"," "))
s=d.get("credsStore") or ""
if s:
    print("store\t"+str(s).replace("\t"," ").replace("\n"," "))
`
