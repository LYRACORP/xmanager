package components

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// Truncate shortens s to at most max display columns, appending an ellipsis when needed.
func Truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return runewidth.Truncate(s, max-1, "") + "…"
}

// Wrap soft-wraps plain text to width columns (rune-aware). Empty width returns s unchanged.
func Wrap(s string, width int) string {
	if width < 8 {
		return Truncate(s, width)
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var lines []string
	for _, para := range strings.Split(s, "\n") {
		lines = append(lines, wrapParagraph(para, width)...)
	}
	return strings.Join(lines, "\n")
}

func wrapParagraph(para string, width int) []string {
	para = strings.TrimRight(para, " ")
	if para == "" {
		return []string{""}
	}
	if runewidth.StringWidth(para) <= width {
		return []string{para}
	}
	words := strings.Fields(para)
	if len(words) == 0 {
		return []string{Truncate(para, width)}
	}
	var out []string
	var line string
	for _, w := range words {
		candidate := w
		if line != "" {
			candidate = line + " " + w
		}
		if runewidth.StringWidth(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			out = append(out, line)
		}
		if runewidth.StringWidth(w) > width {
			out = append(out, breakLongWord(w, width)...)
			line = ""
			continue
		}
		line = w
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

func breakLongWord(w string, width int) []string {
	var parts []string
	for runewidth.StringWidth(w) > width {
		cut := width
		// walk runes until display width reaches cut
		var b strings.Builder
		ww := 0
		for len(w) > 0 {
			r, size := utf8.DecodeRuneInString(w)
			rw := runewidth.RuneWidth(r)
			if ww+rw > cut && ww > 0 {
				break
			}
			b.WriteRune(r)
			ww += rw
			w = w[size:]
		}
		parts = append(parts, b.String())
	}
	if w != "" {
		parts = append(parts, w)
	}
	return parts
}

// FitWidth clamps rendered content to max columns (truncates each visual line).
func FitWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, Truncate(line, max))
	}
	return strings.Join(out, "\n")
}

// TruncateMiddle keeps head and tail of long paths/strings.
func TruncateMiddle(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= max {
		return s
	}
	if max <= 3 {
		return Truncate(s, max)
	}
	keep := (max - 1) / 2
	head := runewidth.Truncate(s, keep, "")
	// take tail by walking from end
	tail := ""
	tw := 0
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		rw := runewidth.RuneWidth(r)
		if tw+rw > keep {
			break
		}
		tail = string(r) + tail
		tw += rw
		s = s[:len(s)-size]
	}
	return head + "…" + tail
}
