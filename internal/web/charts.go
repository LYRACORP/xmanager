package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/dbmanager"
	"github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/storage"
)

var chartDBEngines = []dbmanager.DBType{
	dbmanager.PostgreSQL,
	dbmanager.MySQL,
	dbmanager.MariaDB,
	dbmanager.MongoDB,
	dbmanager.Redis,
	dbmanager.ClickHouse,
}

func (h *handler) startChartSampler() {
	if h.opts.DB == nil || h.localSrvID == 0 {
		return
	}
	h.chartStop = make(chan struct{})
	h.persistChartSamples()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-h.chartStop:
				return
			case <-t.C:
				h.persistChartSamples()
			}
		}
	}()
}

func (h *handler) stopChartSampler() {
	if h.chartStop == nil {
		return
	}
	select {
	case <-h.chartStop:
	default:
		close(h.chartStop)
	}
}

func (h *handler) persistChartSamples() {
	if h.opts.DB == nil || h.localSrvID == 0 {
		return
	}
	now := time.Now()
	if h.node != nil {
		snap := h.node.Latest()
		_ = storage.Record(h.opts.DB, storage.MetricSample{
			ServerID:  h.localSrvID,
			Kind:      storage.KindHost,
			SampledAt: now,
			CPUPct:    snap.CPUPct,
			RAMPct:    snap.RAMPct,
			DiskPct:   snap.DiskPct,
			NetRxBps:  snap.NetRxBps,
			NetTxBps:  snap.NetTxBps,
		})
	}

	exec := h.localExec()
	var names []string
	for _, t := range chartDBEngines {
		if n := dbmanager.ContainerName(t); n != "" && dbmanager.ContainerRunning(exec, n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return
	}
	stats := docker.NewManager(exec).Stats(names)
	for _, t := range chartDBEngines {
		name := dbmanager.ContainerName(t)
		st, ok := stats[name]
		if !ok {
			continue
		}
		_ = storage.Record(h.opts.DB, storage.MetricSample{
			ServerID:  h.localSrvID,
			Kind:      storage.KindDB(string(t)),
			SampledAt: now,
			CPUPct:    parseCPUPct(st.CPUPct),
			MemMB:     parseMemMB(st.MemUsage),
		})
	}
}

func parseCPUPct(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func parseMemMB(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "—" {
		return 0
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.ReplaceAll(s, " ", "")
	mult := 1.0
	switch {
	case strings.HasSuffix(strings.ToLower(s), "gib"):
		mult = 1024
		s = s[:len(s)-3]
	case strings.HasSuffix(strings.ToLower(s), "mib"):
		s = s[:len(s)-3]
	case strings.HasSuffix(strings.ToLower(s), "kib"):
		mult = 1.0 / 1024
		s = s[:len(s)-3]
	case strings.HasSuffix(strings.ToLower(s), "gb"):
		mult = 1024
		s = s[:len(s)-2]
	case strings.HasSuffix(strings.ToLower(s), "mb"):
		s = s[:len(s)-2]
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v * mult
}

func writeChartJSON(w http.ResponseWriter, rng storage.ChartRange, ser storage.ChartSeries) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"range":  rng.Key,
		"t":      ser.T,
		"cpu":    ser.CPU,
		"ram":    ser.RAM,
		"disk":   ser.Disk,
		"rx_bps": ser.RxBps,
		"tx_bps": ser.TxBps,
		"mem":    ser.MemMB,
	})
}

func (h *handler) getNodeCharts(w http.ResponseWriter, r *http.Request) {
	rng := storage.ParseChartRange(r.URL.Query().Get("range"))
	if rng.Dur == 0 {
		writeChartJSON(w, rng, storage.ChartSeries{})
		return
	}
	rows, err := storage.ListChart(h.opts.DB, h.localServerID(), storage.KindHost, time.Now().Add(-rng.Dur), 300)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeChartJSON(w, rng, storage.SeriesFromSamples(rows))
}

func (h *handler) getDatabaseCharts(w http.ResponseWriter, r *http.Request) {
	rng := storage.ParseChartRange(r.URL.Query().Get("range"))
	engine := strings.ToLower(strings.TrimSpace(r.PathValue("type")))
	if rng.Dur == 0 || engine == "" {
		writeChartJSON(w, rng, storage.ChartSeries{})
		return
	}
	rows, err := storage.ListChart(h.opts.DB, h.localServerID(), storage.KindDB(engine), time.Now().Add(-rng.Dur), 300)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeChartJSON(w, rng, storage.SeriesFromSamples(rows))
}
