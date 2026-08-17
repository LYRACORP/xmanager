package storage

import (
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	KindHost = "host"

	chartMaxRowsPerKind = 20000
	chartRawKeep        = 6 * time.Hour
	chartFiveMinKeep    = 7 * 24 * time.Hour
	chartHourKeep       = 365 * 24 * time.Hour
	chartMaxPts         = 300
)

// KindDB returns the sample kind for a database engine (e.g. db:postgres).
func KindDB(engine string) string {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		return "db"
	}
	return "db:" + engine
}

// ChartRange is a named lookback for panel charts.
type ChartRange struct {
	Key string
	Dur time.Duration // 0 = live (no DB series)
}

var chartRanges = []ChartRange{
	{Key: "live"},
	{Key: "1h", Dur: time.Hour},
	{Key: "6h", Dur: 6 * time.Hour},
	{Key: "1d", Dur: 24 * time.Hour},
	{Key: "1w", Dur: 7 * 24 * time.Hour},
	{Key: "1m", Dur: 30 * 24 * time.Hour},
	{Key: "1y", Dur: 365 * 24 * time.Hour},
}

// ParseChartRange maps live|1h|6h|1d|1w|1m|1y. Unknown keys fall back to live.
func ParseChartRange(s string) ChartRange {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ChartRange{Key: "live"}
	}
	for _, r := range chartRanges {
		if r.Key == s {
			return r
		}
	}
	return ChartRange{Key: "live"}
}

// ChartSeries is a downsampled time series for canvases.
type ChartSeries struct {
	T     []int64   `json:"t"`
	CPU   []float64 `json:"cpu"`
	RAM   []float64 `json:"ram"`
	Disk  []float64 `json:"disk"`
	RxBps []float64 `json:"rx_bps"`
	TxBps []float64 `json:"tx_bps"`
	MemMB []float64 `json:"mem"`
}

// Record inserts a sample and downsamples that kind.
func Record(db *gorm.DB, s MetricSample) error {
	if db == nil {
		return nil
	}
	if s.SampledAt.IsZero() {
		s.SampledAt = time.Now()
	}
	if s.Kind == "" {
		s.Kind = KindHost
	}
	if err := db.Create(&s).Error; err != nil {
		return err
	}
	Downsample(db, s.ServerID, s.Kind)
	return nil
}

// Downsample collapses old samples and enforces retention.
func Downsample(db *gorm.DB, serverID uint, kind string) {
	if db == nil || serverID == 0 || kind == "" {
		return
	}
	now := time.Now()
	_ = db.Where("server_id = ? AND kind = ? AND sampled_at < ?", serverID, kind, now.Add(-chartHourKeep)).
		Delete(&MetricSample{}).Error

	collapseBuckets(db, serverID, kind, now.Add(-chartHourKeep), now.Add(-chartFiveMinKeep), time.Hour)
	collapseBuckets(db, serverID, kind, now.Add(-chartFiveMinKeep), now.Add(-chartRawKeep), 5*time.Minute)

	var count int64
	_ = db.Model(&MetricSample{}).Where("server_id = ? AND kind = ?", serverID, kind).Count(&count).Error
	if count <= chartMaxRowsPerKind {
		return
	}
	excess := int(count) - chartMaxRowsPerKind
	var old []MetricSample
	_ = db.Where("server_id = ? AND kind = ?", serverID, kind).
		Order("sampled_at ASC").Limit(excess).Find(&old).Error
	if len(old) == 0 {
		return
	}
	ids := make([]uint, len(old))
	for i, s := range old {
		ids[i] = s.ID
	}
	_ = db.Where("id IN ?", ids).Delete(&MetricSample{}).Error
}

func collapseBuckets(db *gorm.DB, serverID uint, kind string, from, to time.Time, bucket time.Duration) {
	if !to.After(from) || bucket <= 0 {
		return
	}
	var rows []MetricSample
	if err := db.Where("server_id = ? AND kind = ? AND sampled_at >= ? AND sampled_at < ?",
		serverID, kind, from, to).Order("sampled_at ASC").Find(&rows).Error; err != nil || len(rows) < 2 {
		return
	}
	type acc struct {
		n                           int
		cpu, ram, disk, rx, tx, mem float64
		ids                         []uint
		at                          time.Time
	}
	groups := map[int64]*acc{}
	var keys []int64
	sec := int64(bucket.Seconds())
	if sec <= 0 {
		return
	}
	for _, r := range rows {
		k := r.SampledAt.Unix() / sec
		g, ok := groups[k]
		if !ok {
			g = &acc{at: time.Unix(k*sec, 0).UTC()}
			groups[k] = g
			keys = append(keys, k)
		}
		g.n++
		g.cpu += r.CPUPct
		g.ram += r.RAMPct
		g.disk += r.DiskPct
		g.rx += r.NetRxBps
		g.tx += r.NetTxBps
		g.mem += r.MemMB
		g.ids = append(g.ids, r.ID)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		g := groups[k]
		if g.n < 2 {
			continue
		}
		n := float64(g.n)
		_ = db.Where("id IN ?", g.ids).Delete(&MetricSample{}).Error
		_ = db.Create(&MetricSample{
			ServerID:  serverID,
			Kind:      kind,
			SampledAt: g.at,
			CPUPct:    g.cpu / n,
			RAMPct:    g.ram / n,
			DiskPct:   g.disk / n,
			NetRxBps:  g.rx / n,
			NetTxBps:  g.tx / n,
			MemMB:     g.mem / n,
		}).Error
	}
}

// ListChart returns samples since `since`, bucketed to at most maxPts points.
func ListChart(db *gorm.DB, serverID uint, kind string, since time.Time, maxPts int) ([]MetricSample, error) {
	if db == nil {
		return nil, nil
	}
	if maxPts <= 0 || maxPts > chartMaxPts {
		maxPts = chartMaxPts
	}
	q := db.Where("server_id = ? AND kind = ?", serverID, kind)
	if !since.IsZero() {
		q = q.Where("sampled_at >= ?", since)
	}
	var rows []MetricSample
	if err := q.Order("sampled_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return bucketSamples(rows, maxPts), nil
}

func bucketSamples(rows []MetricSample, maxPts int) []MetricSample {
	if len(rows) <= maxPts {
		return rows
	}
	if maxPts < 1 {
		return nil
	}
	out := make([]MetricSample, 0, maxPts)
	n := len(rows)
	for i := 0; i < maxPts; i++ {
		start := i * n / maxPts
		end := (i + 1) * n / maxPts
		if end <= start {
			end = start + 1
		}
		if start >= n {
			break
		}
		if end > n {
			end = n
		}
		var sum MetricSample
		cnt := 0
		for _, r := range rows[start:end] {
			sum.CPUPct += r.CPUPct
			sum.RAMPct += r.RAMPct
			sum.DiskPct += r.DiskPct
			sum.NetRxBps += r.NetRxBps
			sum.NetTxBps += r.NetTxBps
			sum.MemMB += r.MemMB
			cnt++
		}
		if cnt == 0 {
			continue
		}
		c := float64(cnt)
		mid := rows[start+(cnt-1)/2]
		out = append(out, MetricSample{
			ServerID:  mid.ServerID,
			Kind:      mid.Kind,
			SampledAt: mid.SampledAt,
			CPUPct:    sum.CPUPct / c,
			RAMPct:    sum.RAMPct / c,
			DiskPct:   sum.DiskPct / c,
			NetRxBps:  sum.NetRxBps / c,
			NetTxBps:  sum.NetTxBps / c,
			MemMB:     sum.MemMB / c,
		})
	}
	return out
}

// SeriesFromSamples maps rows to JSON-ready arrays.
func SeriesFromSamples(rows []MetricSample) ChartSeries {
	s := ChartSeries{
		T:     make([]int64, len(rows)),
		CPU:   make([]float64, len(rows)),
		RAM:   make([]float64, len(rows)),
		Disk:  make([]float64, len(rows)),
		RxBps: make([]float64, len(rows)),
		TxBps: make([]float64, len(rows)),
		MemMB: make([]float64, len(rows)),
	}
	for i, r := range rows {
		s.T[i] = r.SampledAt.Unix()
		s.CPU[i] = r.CPUPct
		s.RAM[i] = r.RAMPct
		s.Disk[i] = r.DiskPct
		s.RxBps[i] = r.NetRxBps
		s.TxBps[i] = r.NetTxBps
		s.MemMB[i] = r.MemMB
	}
	return s
}
