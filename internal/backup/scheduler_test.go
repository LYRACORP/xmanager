package backup

import (
	"testing"
	"time"

	"github.com/lyracorp/xmanager/internal/storage"
)

func TestValidSchedule(t *testing.T) {
	ok := []string{"@hourly", "@daily", "@weekly", "@monthly", "6h", "30m", "12h"}
	for _, s := range ok {
		if !ValidSchedule(s) {
			t.Fatalf("expected valid: %q", s)
		}
	}
	if ValidSchedule("") || ValidSchedule("bogus") || ValidSchedule("0h") {
		t.Fatal("expected invalid")
	}
}

func TestIsDue(t *testing.T) {
	now := time.Now()
	if !IsDue(storage.Backup{Schedule: "@hourly"}, now) {
		t.Fatal("zero BackedAt should be due")
	}
	recent := storage.Backup{Schedule: "@hourly", BackedAt: now.Add(-30 * time.Minute)}
	if IsDue(recent, now) {
		t.Fatal("should not be due yet")
	}
	old := storage.Backup{Schedule: "@hourly", BackedAt: now.Add(-2 * time.Hour)}
	if !IsDue(old, now) {
		t.Fatal("should be due")
	}
	daily := storage.Backup{Schedule: "@daily", BackedAt: now.Add(-25 * time.Hour)}
	if !IsDue(daily, now) {
		t.Fatal("daily should be due")
	}
}

func TestParseDestIDs(t *testing.T) {
	got := parseDestIDs("local,3,dest:5,ftp:7")
	if len(got) != 3 || got[0] != 3 || got[1] != 5 || got[2] != 7 {
		t.Fatalf("got %#v", got)
	}
}
