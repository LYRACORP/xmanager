package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type Gauge struct {
	Label   string
	Value   float64 // 0.0 to 1.0
	Width   int     // total columns for label + bar + pct
	ShowPct bool
}

func NewGauge(label string, value float64) Gauge {
	return Gauge{
		Label:   label,
		Value:   value,
		Width:   20,
		ShowPct: true,
	}
}

func (g Gauge) View() string {
	if g.Value < 0 {
		g.Value = 0
	}
	if g.Value > 1 {
		g.Value = 1
	}
	if g.Width < 10 {
		g.Width = 10
	}

	labelW := 4
	pctW := 0
	if g.ShowPct {
		pctW = 5 // " 100%"
	}
	// label + space + "[" + bar + "]" + optional pct
	overhead := labelW + 1 + 2 + pctW
	barWidth := g.Width - overhead
	if barWidth < 3 {
		barWidth = 3
	}

	filled := int(float64(barWidth) * g.Value)
	if filled > barWidth {
		filled = barWidth
	}

	var color lipgloss.Color
	switch {
	case g.Value >= 0.9:
		color = theme.Current.Critical
	case g.Value >= 0.75:
		color = theme.Current.Warning
	default:
		color = theme.Current.Success
	}

	fillChar := theme.GaugeFillChar()
	emptyChar := theme.GaugeEmptyChar()
	fillStyle := lipgloss.NewStyle().Foreground(color)
	emptyStyle := lipgloss.NewStyle().Foreground(theme.Current.Muted)

	bar := "[" + fillStyle.Render(repeat(fillChar, filled)) + emptyStyle.Render(repeat(emptyChar, barWidth-filled)) + "]"

	label := lipgloss.NewStyle().
		Foreground(theme.Current.Text).
		Width(labelW).
		Render(Truncate(g.Label, labelW))

	pct := ""
	if g.ShowPct {
		pct = fmt.Sprintf(" %3.0f%%", g.Value*100)
	}

	return label + " " + bar + pct
}

func repeat(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
