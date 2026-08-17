package storage

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openMetricDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&MetricSample{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestParseChartRange(t *testing.T) {
	if ParseChartRange("").Key != "live" || ParseChartRange("nope").Dur != 0 {
		t.Fatal("default live")
	}
	if ParseChartRange("1h").Dur != time.Hour {
		t.Fatal("1h")
	}
	if ParseChartRange("1Y").Dur != 365*24*time.Hour {
		t.Fatal("1y")
	}
	if KindDB("Postgres") != "db:postgres" {
		t.Fatal(KindDB("Postgres"))
	}
}

func TestDownsampleBuckets(t *testing.T) {
	db := openMetricDB(t)
	now := time.Now()
	base := MetricSample{ServerID: 1, Kind: KindHost, CPUPct: 10}

	for i := 0; i < 6; i++ {
		s := base
		s.SampledAt = now.Add(-7*time.Hour - time.Duration(i)*30*time.Second)
		s.CPUPct = float64(10 + i)
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		s := base
		s.SampledAt = now.Add(-10*24*time.Hour - time.Duration(i)*10*time.Minute)
		s.CPUPct = 50
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	recent := base
	recent.SampledAt = now.Add(-time.Hour)
	recent.CPUPct = 3
	if err := db.Create(&recent).Error; err != nil {
		t.Fatal(err)
	}

	Downsample(db, 1, KindHost)

	var n int64
	db.Model(&MetricSample{}).Where("server_id = ? AND kind = ?", 1, KindHost).Count(&n)
	if n > 6 {
		t.Fatalf("expected collapse, got %d rows", n)
	}
	var kept MetricSample
	if err := db.Where("cpu_pct = ?", 3).First(&kept).Error; err != nil {
		t.Fatal("recent raw sample should remain")
	}

	old := MetricSample{ServerID: 1, Kind: KindHost, SampledAt: now.Add(-400 * 24 * time.Hour), CPUPct: 1}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	Downsample(db, 1, KindHost)
	var gone int64
	db.Model(&MetricSample{}).Where("cpu_pct = ?", 1).Count(&gone)
	if gone != 0 {
		t.Fatal("expected 400d sample deleted")
	}
}

func TestListChartMaxPts(t *testing.T) {
	db := openMetricDB(t)
	now := time.Now()
	for i := 0; i < 500; i++ {
		s := MetricSample{
			ServerID:  2,
			Kind:      KindHost,
			SampledAt: now.Add(-time.Duration(500-i) * time.Second),
			CPUPct:    float64(i),
		}
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := ListChart(db, 2, KindHost, now.Add(-time.Hour), 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) > 40 {
		t.Fatalf("len=%d", len(rows))
	}
	if len(rows) < 10 {
		t.Fatalf("too few %d", len(rows))
	}
	ser := SeriesFromSamples(rows)
	if len(ser.T) != len(rows) || len(ser.CPU) != len(rows) {
		t.Fatal("series mismatch")
	}
}

func TestRecordAndKindDB(t *testing.T) {
	db := openMetricDB(t)
	if err := Record(db, MetricSample{ServerID: 3, Kind: KindDB("mysql"), CPUPct: 2, MemMB: 128}); err != nil {
		t.Fatal(err)
	}
	rows, err := ListChart(db, 3, KindDB("mysql"), time.Now().Add(-time.Hour), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%d %v", len(rows), err)
	}
	if rows[0].MemMB != 128 {
		t.Fatalf("%v", rows[0])
	}
}
