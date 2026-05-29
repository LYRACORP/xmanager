package components

import (
	"strings"

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

	content := strings.Join(parts, "  ")
	return theme.AppFooterStyle(h.Width).Render(content)
}
