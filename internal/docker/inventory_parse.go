package docker

import (
	"encoding/json"
	"strconv"
	"strings"
)

type verboseDF struct {
	Volumes []Volume
	Cache   []BuildCache
}

// ParseLoginProbe parses output of loginProbeCmd.
func ParseLoginProbe(stdout string) LoginInfo {
	stdout = strings.ReplaceAll(stdout, "\r\n", "\n")
	userPart, authPart, ok := strings.Cut(stdout, "---XM---")
	if !ok {
		userPart = stdout
	}
	user := strings.TrimSpace(userPart)
	if user == "<no value>" || strings.EqualFold(user, "<none>") || strings.EqualFold(user, "<nil>") {
		user = ""
	}
	info := LoginInfo{
		Username: user,
	}
	for _, line := range strings.Split(authPart, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kind, rest, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		rest = prettyRegistry(strings.TrimSpace(rest))
		if rest == "" {
			continue
		}
		switch strings.TrimSpace(kind) {
		case "auth", "helper":
			info.Registries = append(info.Registries, rest)
		case "store":
			info.CredsStore = rest
		}
	}
	info.Registries = uniqueNonEmpty(info.Registries)
	if info.Username != "" {
		// Hub user implies docker.io; keep it first without duplicating.
		hasHub := false
		for _, r := range info.Registries {
			if r == "docker.io" {
				hasHub = true
				break
			}
		}
		if !hasHub {
			info.Registries = append([]string{"docker.io"}, info.Registries...)
		}
	}
	return info
}

func prettyRegistry(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "/")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimRight(s, "/")
	if s == "index.docker.io/v1" || s == "index.docker.io" || strings.HasPrefix(s, "index.docker.io/") {
		return "docker.io"
	}
	if s == "registry-1.docker.io" {
		return "docker.io"
	}
	return s
}

// ParseVolumeLSJSON parses `docker volume ls --format '{{json .}}'` (NDJSON).
func ParseVolumeLSJSON(stdout string) ([]Volume, error) {
	var out []Volume
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			Name   string `json:"Name"`
			Driver string `json:"Driver"`
			Scope  string `json:"Scope"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw.Name == "" {
			continue
		}
		out = append(out, Volume{Name: raw.Name, Driver: raw.Driver, Scope: raw.Scope})
	}
	return out, nil
}

// ParseNetworkInspect parses tab-separated `docker network inspect --format` lines.
func ParseNetworkInspect(stdout string) []Network {
	var out []Network
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 6 {
			continue
		}
		n := Network{
			Name:       strings.TrimSpace(parts[0]),
			ID:         strings.TrimSpace(parts[1]),
			Driver:     strings.TrimSpace(parts[2]),
			Scope:      strings.TrimSpace(parts[3]),
			Internal:   strings.EqualFold(strings.TrimSpace(parts[4]), "yes"),
			Containers: atoiDef(parts[5]),
		}
		if len(parts) > 6 {
			n.Subnet = strings.TrimSpace(parts[6])
		}
		if n.Name == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ParseVerboseDF extracts volume sizes and build-cache rows from `docker system df -v`.
func ParseVerboseDF(stdout string) verboseDF {
	var df verboseDF
	section := ""
	headerSeen := false
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "local volumes space usage"):
			section = "volumes"
			headerSeen = false
			continue
		case strings.HasPrefix(lower, "build cache"):
			section = "cache"
			headerSeen = false
			continue
		case strings.HasPrefix(lower, "images space"), strings.HasPrefix(lower, "containers space"):
			section = ""
			headerSeen = false
			continue
		}
		if section == "" || line == "" {
			continue
		}
		if isVolumeHeader(line) || isCacheHeader(line) {
			headerSeen = true
			continue
		}
		if !headerSeen {
			// Some docker versions print only "Build cache usage: 0B" with no table.
			continue
		}
		switch section {
		case "volumes":
			if v, ok := parseVolumeDFLine(line); ok {
				df.Volumes = append(df.Volumes, v)
			}
		case "cache":
			if c, ok := parseCacheDFLine(line); ok {
				df.Cache = append(df.Cache, c)
			}
		}
	}
	return df
}

func isVolumeHeader(line string) bool {
	u := strings.ToUpper(line)
	return strings.Contains(u, "VOLUME NAME") && strings.Contains(u, "SIZE")
}

func isCacheHeader(line string) bool {
	u := strings.ToUpper(line)
	return strings.Contains(u, "CACHE TYPE") && strings.Contains(u, "SIZE")
}

func parseVolumeDFLine(line string) (Volume, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return Volume{}, false
	}
	size := fields[len(fields)-1]
	links, err := strconv.Atoi(fields[len(fields)-2])
	if err != nil {
		return Volume{}, false
	}
	name := strings.Join(fields[:len(fields)-2], " ")
	if name == "" {
		return Volume{}, false
	}
	return Volume{
		Name:      name,
		Links:     links,
		SizeHuman: size,
		SizeBytes: ParseDockerSize(size),
	}, true
}

func parseCacheDFLine(line string) (BuildCache, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return BuildCache{}, false
	}
	sharedStr := strings.ToLower(fields[len(fields)-1])
	if sharedStr != "true" && sharedStr != "false" {
		return BuildCache{}, false
	}
	usage, err := strconv.Atoi(fields[len(fields)-2])
	if err != nil {
		return BuildCache{}, false
	}
	id := strings.TrimSuffix(fields[0], "*")
	typ := fields[1]
	size := fields[2]
	if ParseDockerSize(size) == 0 && size != "0B" && size != "0" && !looksLikeSize(size) {
		return BuildCache{}, false
	}
	return BuildCache{
		ID:          id,
		Type:        typ,
		SizeHuman:   size,
		SizeBytes:   ParseDockerSize(size),
		Usage:       usage,
		Shared:      sharedStr == "true",
		Reclaimable: sharedStr != "true",
	}, true
}

func looksLikeSize(s string) bool {
	if s == "" {
		return false
	}
	s = strings.ToUpper(s)
	for _, suf := range []string{"B", "KB", "MB", "GB", "TB", "KIB", "MIB", "GIB", "TIB", "K", "M", "G", "T"} {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}

// ParseBuildxDU parses `docker buildx du` table output.
func ParseBuildxDU(stdout string) []BuildCache {
	var out []BuildCache
	headerSeen := false
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "RECLAIMABLE") && strings.Contains(upper, "SIZE") {
			headerSeen = true
			continue
		}
		if !headerSeen {
			continue
		}
		// Summary footer: "Shared:", "Private:", "Reclaimable:", "Total:"
		if strings.Contains(line, ":") && !strings.Contains(line, "\t") {
			key := strings.ToLower(strings.TrimSpace(strings.SplitN(line, ":", 2)[0]))
			if key == "shared" || key == "private" || key == "reclaimable" || key == "total" {
				break
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		id := strings.TrimSuffix(fields[0], "*")
		reclaim := strings.ToLower(fields[1])
		size := fields[2]
		if !looksLikeSize(size) && reclaim != "true" && reclaim != "false" {
			continue
		}
		// When RECLAIMABLE is true/false, SIZE is field 2.
		if reclaim != "true" && reclaim != "false" {
			continue
		}
		out = append(out, BuildCache{
			ID:          id,
			Type:        "buildx",
			SizeHuman:   size,
			SizeBytes:   ParseDockerSize(size),
			Reclaimable: reclaim == "true",
		})
	}
	return out
}
