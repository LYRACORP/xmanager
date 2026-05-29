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
	var parts []string
	for _, tab := range t.Tabs {
		label := tab.Label
		if tab.ID == t.Active {
			parts = append(parts, lipgloss.NewStyle().
				Bold(true).
				Foreground(theme.Current.Accent).
				Render(label))
		} else {
			parts = append(parts, theme.MutedText().Render(label))
		}
	}
	line := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	if t.Width > 0 {
		line = lipgloss.NewStyle().Width(t.Width).PaddingLeft(theme.PadSM).Render(line)
	}
	return line
}

func TabBarRows() int {
	return 1
}
