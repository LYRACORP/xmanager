package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type Gauge struct {
	Label   string
	Value   float64 // 0.0 to 1.0
	Width   int
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

	barWidth := g.Width - 2
	if barWidth < 4 {
		barWidth = 4
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

	labelW := 6
	if layout.Breakpoint(g.Width) == layout.BreakpointNarrow {
		labelW = 4
	}
	label := lipgloss.NewStyle().
		Foreground(theme.Current.Text).
		Width(labelW).
		Render(g.Label)

	pct := ""
	if g.ShowPct {
		pct = fmt.Sprintf(" %3.0f%%", g.Value*100)
	}

	return label + " " + bar + pct
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
