package settings

import "testing"

func TestLogRetentionField(t *testing.T) {
	if fieldLogRetention != fieldMaxLogLines+1 {
		t.Fatalf("fieldLogRetention=%d want after max log lines", fieldLogRetention)
	}
	if fieldCount != 14 {
		t.Fatalf("fieldCount=%d want 14", fieldCount)
	}
}
