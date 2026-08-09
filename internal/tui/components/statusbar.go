package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type StatusBar struct {
	ServerName string
	ServerHost string
	Connected  bool
	Width      int
}

func NewStatusBar() StatusBar {
	return StatusBar{}
}

func (s StatusBar) View() string {
	inner := s.Width - 4
	if inner < 8 {
		inner = 8
	}

	style := lipgloss.NewStyle().
		Background(theme.Current.Background).
		Foreground(theme.Current.TextDim).
		Width(s.Width).
		MaxWidth(s.Width).
		Padding(0, theme.PadMD)

	left := theme.MutedText().Render("  no server connected")
	if s.ServerName != "" {
		status := theme.StatusDot(s.Connected)
		name := Truncate(s.ServerName, max(8, inner/3))
		host := Truncate(s.ServerHost, max(8, inner-lipgloss.Width(name)-6))
		left = fmt.Sprintf("  %s %s · %s", status, name, host)
		if lipgloss.Width(left) > inner {
			left = status + " " + Truncate(name+" · "+host, inner-2)
		}
	}

	return style.Render(left)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
