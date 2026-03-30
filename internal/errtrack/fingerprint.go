package errtrack

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

var (
	timestampRe = regexp.MustCompile(`\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}`)
	numbersRe   = regexp.MustCompile(`\b\d{4,}\b`)
	hexRe       = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	uuidRe      = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

func Fingerprint(service, message string) string {
	normalized := normalize(message)
	input := service + "::" + normalized
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash[:8])
}

func normalize(message string) string {
	s := message
	s = timestampRe.ReplaceAllString(s, "<TS>")
	s = uuidRe.ReplaceAllString(s, "<UUID>")
	s = hexRe.ReplaceAllString(s, "<HEX>")
	s = numbersRe.ReplaceAllString(s, "<N>")

	lines := strings.Split(s, "\n")
	if len(lines) > 5 {
		lines = lines[:5]
	}
	return strings.Join(lines, "\n")
}
