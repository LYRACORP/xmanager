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
	style := lipgloss.NewStyle().
		Background(theme.Current.Surface).
		Foreground(theme.Current.Text).
		Width(s.Width).
		Padding(0, 1)

	left := ""
	if s.ServerName != "" {
		status := theme.StatusDot(s.Connected)
		left = fmt.Sprintf("%s %s (%s)", status, s.ServerName, s.ServerHost)
	} else {
		left = theme.MutedText().Render("No server connected")
	}

	right := fmt.Sprintf("XManager %s", "")

	gap := s.Width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}

	return style.Render(left + fmt.Sprintf("%*s", gap, "") + right)
}
