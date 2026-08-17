package settings

import "testing"

func TestLogRetentionField(t *testing.T) {
	if fieldLogRetention != fieldMaxLogLines+1 {
		t.Fatalf("fieldLogRetention=%d want after max log lines", fieldLogRetention)
	}
	if fieldCount != 15 {
		t.Fatalf("fieldCount=%d want 15", fieldCount)
	}
}
