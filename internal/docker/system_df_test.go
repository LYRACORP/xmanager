package docker

import "testing"

func TestParseDockerSize(t *testing.T) {
	f := 1.234 * float64(1024*1024*1024)
	want1234GB := uint64(f)
	cases := []struct {
		in   string
		want uint64
	}{
		{"512B", 512},
		{"1KB", 1024},
		{"1.5MB", uint64(1.5 * 1024 * 1024)},
		{"2GB", 2 * 1024 * 1024 * 1024},
		{"1.234GB", want1234GB},
		{"500MB (50%)", 500 * 1024 * 1024},
		{"", 0},
		{"-", 0},
	}
	for _, tc := range cases {
		got := ParseDockerSize(tc.in)
		if got != tc.want {
			t.Fatalf("%q: got %d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseSystemDF(t *testing.T) {
	stdout := `{"Type":"Images","TotalCount":"3","Active":"2","Size":"1.5GB","Reclaimable":"500MB (33%)"}
{"Type":"Local Volumes","TotalCount":"4","Active":"1","Size":"200MB","Reclaimable":"0B (0%)"}
{"Type":"Build Cache","TotalCount":"10","Active":"0","Size":"1GB","Reclaimable":"1GB"}
`
	rows, err := ParseSystemDF(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows=%d", len(rows))
	}
	if DFBytesByType(rows, "Images") != ParseDockerSize("1.5GB") {
		t.Fatalf("images=%d", DFBytesByType(rows, "Images"))
	}
	if DFBytesByType(rows, "Volumes") != ParseDockerSize("200MB") {
		t.Fatalf("vols=%d", DFBytesByType(rows, "Volumes"))
	}
	if DFBytesByType(rows, "Build Cache") != ParseDockerSize("1GB") {
		t.Fatalf("cache=%d", DFBytesByType(rows, "Build Cache"))
	}
}
