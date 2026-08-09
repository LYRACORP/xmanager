package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

// StatChip renders a compact label + value chip for dashboard metrics.
func StatChip(label, value string) string {
	value = Truncate(value, 28)
	lbl := lipgloss.NewStyle().
		Foreground(theme.Current.Muted).
		Render(label + " ")
	val := lipgloss.NewStyle().
		Foreground(theme.Current.Text).
		Bold(true).
		Render(value)
	return theme.ChipStyle().Render(lbl + val)
}

// StatRow joins chips horizontally with spacing.
func StatRow(chips ...string) string {
	if len(chips) == 0 {
		return ""
	}
	out := chips[0]
	for i := 1; i < len(chips); i++ {
		out += " " + chips[i]
	}
	return out
}

// StatRowWrap joins chips, wrapping to the next line when width is exceeded.
func StatRowWrap(width int, chips ...string) string {
	if len(chips) == 0 {
		return ""
	}
	if width < 8 {
		return StatRow(chips...)
	}
	var lines []string
	var line string
	for _, chip := range chips {
		candidate := chip
		if line != "" {
			candidate = line + " " + chip
		}
		if lipgloss.Width(candidate) <= width {
			line = candidate
			continue
		}
		if line != "" {
			lines = append(lines, line)
		}
		if lipgloss.Width(chip) > width {
			lines = append(lines, FitWidth(chip, width))
			line = ""
			continue
		}
		line = chip
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// HumanBytes formats byte counts for network/disk display.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
