package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

// StatChip renders a compact label + value chip for dashboard metrics.
func StatChip(label, value string) string {
	lbl := lipgloss.NewStyle().
		Foreground(theme.Current.Muted).
		Render(label + " ")
	val := lipgloss.NewStyle().
		Foreground(theme.Current.Text).
		Bold(true).
		Render(value)
	return theme.PanelStyle().
		Padding(0, 1).
		Render(lbl + val)
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
