package activity

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/lyracorp/xmanager/internal/storage"
	"gorm.io/gorm"
)

func TestLogAndList(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&storage.ActivityLog{}); err != nil {
		t.Fatal(err)
	}
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
