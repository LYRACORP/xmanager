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
		Background(theme.Current.Background).
		Foreground(theme.Current.TextDim).
		Width(s.Width).
		Padding(0, theme.PadMD)

	left := theme.MutedText().Render("  no server connected")
	if s.ServerName != "" {
		status := theme.StatusDot(s.Connected)
		left = fmt.Sprintf("  %s %s · %s", status, s.ServerName, s.ServerHost)
	}

	return style.Render(left)
}
