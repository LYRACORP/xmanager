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
	Loading    bool
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
	if s.Loading && s.ServerName == "" {
		dot := lipgloss.NewStyle().Foreground(theme.Current.Accent).Render("●")
		left = fmt.Sprintf("  %s %s", dot, theme.MutedText().Render("loading…"))
	} else if s.ServerName != "" {
		var status string
		if s.Loading {
			status = lipgloss.NewStyle().Foreground(theme.Current.Accent).Render("●")
			name := Truncate(s.ServerName, max(8, inner/3))
			host := Truncate(s.ServerHost, max(8, inner-lipgloss.Width(name)-16))
			left = fmt.Sprintf("  %s %s · %s · %s", status, name, host, theme.MutedText().Render("loading…"))
		} else {
			status = theme.StatusDot(s.Connected)
			name := Truncate(s.ServerName, max(8, inner/3))
			host := Truncate(s.ServerHost, max(8, inner-lipgloss.Width(name)-6))
			left = fmt.Sprintf("  %s %s · %s", status, name, host)
		}
		if lipgloss.Width(left) > inner {
			if s.Loading {
				left = status + " " + Truncate(s.ServerName+" · loading…", inner-2)
			} else {
				left = status + " " + Truncate(s.ServerName+" · "+s.ServerHost, inner-2)
			}
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
