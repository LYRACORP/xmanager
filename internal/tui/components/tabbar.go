package components

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type TabItem struct {
	ID    int
	Label string
}

type TabBar struct {
	Tabs   []TabItem
	Active int
	Width  int
}

func NewTabBar(tabs []TabItem, active int) TabBar {
	return TabBar{Tabs: tabs, Active: active}
}

func (t TabBar) View() string {
	inner := t.Width - theme.PadSM
	if inner < 8 {
		inner = 8
	}

	var rendered []string
	used := 0
	for _, tab := range t.Tabs {
		var part string
		if tab.ID == t.Active {
			part = lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.Current.Accent).
				Render(tab.Label)
		} else {
			part = theme.MutedText().Render(tab.Label)
		}
		w := lipgloss.Width(part)
		sep := 0
		if len(rendered) > 0 {
			sep = 2
		}
		if t.Width > 0 && used+sep+w > inner {
			if len(rendered) == 0 {
				rendered = append(rendered, theme.MutedText().Render(Truncate(tab.Label, inner)))
			}
			break
		}
		rendered = append(rendered, part)
		used += sep + w
	}

	line := ""
	for i, part := range rendered {
		if i > 0 {
			line += "  "
		}
		line += part
	}
	if t.Width > 0 {
		line = lipgloss.NewStyle().Width(t.Width).MaxWidth(t.Width).PaddingLeft(theme.PadSM).Render(line)
	}
	return line
}

func TabBarRows() int {
	return 1
}
