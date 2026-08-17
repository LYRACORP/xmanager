package traffic

import (
	"regexp"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

// combined log: IP - - [date] "METHOD path PROTO" status size "referer" "ua"
var nginxCombinedRe = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "(\S+) ([^"]*?) (\S+)" (\d+) `)

// ParseNginxAccessLine parses one nginx combined log line.
func ParseNginxAccessLine(line string) (ip, method, path string, status int, ok bool) {
	line = strings.TrimSpace(line)
	m := nginxCombinedRe.FindStringSubmatch(line)
	if len(m) < 7 {
		return "", "", "", 0, false
	}
	ip = m[1]
	method = m[3]
	path = m[4]
	if idx := strings.Index(path, "?"); idx >= 0 {
		path = path[:idx]
	}
	if _, err := parseInt(m[6], &status); err != nil {
		status = 0
	}
	return ip, method, path, status, true
}

func parseInt(s string, out *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	*out = n
	return n, nil
}

// IngestNginxLines parses and records nginx access log lines.
func IngestNginxLines(serverID uint, lines string, rec *Recorder) {
	if rec == nil || !rec.analysisOn() {
		return
	}
	for _, line := range strings.Split(lines, "\n") {
		ip, method, path, status, ok := ParseNginxAccessLine(line)
		if !ok {
			continue
		}
		rec.Record(Hit{
			ServerID: serverID,
			Source:   SourceNginx,
			IP:       ip,
			Method:   method,
			Path:     path,
			Status:   status,
		})
	}
}

// TopEntry is an aggregated counter row.
type TopEntry struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// SeriesPoint is one bucket in a time series.
type SeriesPoint struct {
	T int64 `json:"t"`
	N int   `json:"n"`
}

// Analysis is traffic summary JSON.
type Analysis struct {
	Series   []SeriesPoint `json:"series"`
	TopIPs   []TopEntry    `json:"top_ips"`
	TopPaths []TopEntry    `json:"top_paths"`
	Blocked  int           `json:"blocked"`
	Allowed  int           `json:"allowed"`
	Panel    int           `json:"panel"`
	Nginx    int           `json:"nginx"`
}

// Analyze builds traffic summary since `since`.
func Analyze(db *gorm.DB, serverID uint, since time.Time) (Analysis, error) {
	var out Analysis
	if db == nil {
		return out, nil
	}
	var rows []storage.TrafficHit
	err := db.Where("server_id = ? AND at >= ?", serverID, since).Order("at asc").Find(&rows).Error
	if err != nil {
		return out, err
	}
	ipCounts := map[string]int{}
	pathCounts := map[string]int{}
	buckets := map[int64]int{}
	for _, r := range rows {
		if r.Blocked {
			out.Blocked++
		} else {
			out.Allowed++
		}
		if r.Source == SourcePanel {
			out.Panel++
		} else if r.Source == SourceNginx {
			out.Nginx++
		}
		ipCounts[r.IP]++
		pathCounts[r.Path]++
		bucket := r.At.Unix() / 60
		buckets[bucket]++
	}
	out.TopIPs = topN(ipCounts, 10)
	out.TopPaths = topN(pathCounts, 10)
	out.Series = seriesFromBuckets(buckets, 300)
	return out, nil
}

func topN(m map[string]int, n int) []TopEntry {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range m {
		list = append(list, kv{k, v})
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].v > list[i].v {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	if len(list) > n {
		list = list[:n]
	}
	var out []TopEntry
	for _, e := range list {
		out = append(out, TopEntry{Key: e.k, Count: e.v})
	}
	return out
}

func seriesFromBuckets(buckets map[int64]int, maxPts int) []SeriesPoint {
	if len(buckets) == 0 {
		return nil
	}
	var keys []int64
	for k := range buckets {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	step := 1
	if len(keys) > maxPts {
		step = len(keys) / maxPts
		if step < 1 {
			step = 1
		}
	}
	var out []SeriesPoint
	for i := 0; i < len(keys); i += step {
		end := i + step
		if end > len(keys) {
			end = len(keys)
		}
		sum := 0
		for _, k := range keys[i:end] {
			sum += buckets[k]
		}
		out = append(out, SeriesPoint{T: keys[i] * 60, N: sum})
	}
	return out
}
