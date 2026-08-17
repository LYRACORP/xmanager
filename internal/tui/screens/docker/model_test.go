package docker

import "testing"

func TestTabSwarmIsSeven(t *testing.T) {
	if tabSwarm != 6 {
		t.Fatalf("tabSwarm=%d want 6 ([7] key)", tabSwarm)
	}
	if tabCount != 7 {
		t.Fatalf("tabCount=%d want 7", tabCount)
	}
}
