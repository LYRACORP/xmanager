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
	inner := h.Width - 4 // footer padding
	if inner < 12 {
		inner = 12
	}

	var parts []string
	used := 0
	for _, b := range h.Bindings {
		key := theme.KeyStyle().Render(b.Key)
		desc := theme.DescStyle().Render(b.Desc)
		part := key + " " + desc
		w := lipgloss.Width(part)
		sep := 0
		if len(parts) > 0 {
			sep = 2
		}
		if used+sep+w > inner {
			if len(parts) == 0 {
				parts = append(parts, FitWidth(b.Key+" "+b.Desc, inner))
			}
			break
		}
		parts = append(parts, part)
		used += sep + w
	}

	content := strings.Join(parts, "  ")
	return theme.AppFooterStyle(h.Width).MaxWidth(h.Width).Render(content)
}
