package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type KeyBinding struct {
	Key  string
	Desc string
}

type HelpBar struct {
	Bindings []KeyBinding
	Width    int
}

func NewHelpBar(bindings ...KeyBinding) HelpBar {
	return HelpBar{Bindings: bindings}
}

func (h HelpBar) View() string {
	var parts []string
	for _, b := range h.Bindings {
		key := theme.KeyStyle().Render(b.Key)
		desc := theme.DescStyle().Render(b.Desc)
		parts = append(parts, key+" "+desc)
	}

	style := lipgloss.NewStyle().
		Width(h.Width).
		Padding(0, 1).
		Foreground(theme.Current.TextDim)

	return style.Render(strings.Join(parts, "  "))
}
