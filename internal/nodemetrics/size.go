package nodemetrics

import (
	"strconv"
	"strings"
	"unicode"
)

// ParseHumanSize converts strings like "12 MB", "1.5GB", "1024" (bytes) to bytes.
func ParseHumanSize(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0
	}
	// Postgres pg_size_pretty: "128 MB", "8192 bytes"
	s = strings.ReplaceAll(s, "bytes", "B")
	s = strings.ReplaceAll(s, "byte", "B")

	var numStr strings.Builder
	var unitStr strings.Builder
	seenUnit := false
	for _, r := range s {
		if !seenUnit && (unicode.IsDigit(r) || r == '.') {
			numStr.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			seenUnit = true
			continue
		}
		if unicode.IsLetter(r) {
			seenUnit = true
			unitStr.WriteRune(r)
		}
	}
	f, err := strconv.ParseFloat(numStr.String(), 64)
	if err != nil || f < 0 {
		return 0
	}
	unit := strings.ToUpper(unitStr.String())
	mult := float64(1)
	switch unit {
	case "", "B":
		mult = 1
	case "K", "KB", "KIB":
		mult = 1024
	case "M", "MB", "MIB":
		mult = 1024 * 1024
	case "G", "GB", "GIB":
		mult = 1024 * 1024 * 1024
	case "T", "TB", "TIB":
		mult = 1024 * 1024 * 1024 * 1024
	}
	return uint64(f * mult)
}
