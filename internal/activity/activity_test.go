package activity

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func openActivityDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&storage.ActivityLog{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLogAndList(t *testing.T) {
	db := openActivityDB(t)
	Log(db, Entry{
		ServerID: 1,
		Source:   "web",
		Actor:    "admin",
		Action:   "POST",
		Method:   "POST",
		Path:     "/backup/run",
		Detail:   "backup all",
		Status:   "ok",
		IP:       "127.0.0.1",
	})
	rows, err := List(db, Filter{ServerID: 1, Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Path != "/backup/run" {
		t.Fatalf("path=%q", rows[0].Path)
	}
}

func TestTrimOlderThanRetention(t *testing.T) {
	db := openActivityDB(t)
	old := storage.ActivityLog{CreatedAt: time.Now().AddDate(0, 0, -40), ServerID: 1, Path: "/old", Action: "GET", Status: "ok"}
	recent := storage.ActivityLog{CreatedAt: time.Now().AddDate(0, 0, -2), ServerID: 1, Path: "/new", Action: "GET", Status: "ok"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&recent).Error; err != nil {
		t.Fatal(err)
	}
	prev := RetentionDays()
	t.Cleanup(func() { SetRetentionDays(prev) })
	SetRetentionDays(30)
	Trim(db)
	rows, err := List(db, Filter{ServerID: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Path != "/new" {
		t.Fatalf("got %+v", rows)
	}
}

func TestTrimZeroKeepsOld(t *testing.T) {
	db := openActivityDB(t)
	old := storage.ActivityLog{CreatedAt: time.Now().AddDate(0, 0, -90), ServerID: 1, Path: "/ancient", Action: "GET", Status: "ok"}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	prev := RetentionDays()
	t.Cleanup(func() { SetRetentionDays(prev) })
	SetRetentionDays(0)
	Trim(db)
	rows, err := List(db, Filter{ServerID: 1, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected keep old row, got %d", len(rows))
	}
}

func TestSetRetentionDaysClamps(t *testing.T) {
	prev := RetentionDays()
	t.Cleanup(func() { SetRetentionDays(prev) })
	SetRetentionDays(-5)
	if RetentionDays() != 0 {
		t.Fatalf("neg got %d", RetentionDays())
	}
	SetRetentionDays(400)
	if RetentionDays() != 365 {
		t.Fatalf("cap got %d", RetentionDays())
	}
}
