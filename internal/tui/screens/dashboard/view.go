package dashboard

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

func renderDashboard(m *Model) string {
	title := theme.ScreenChrome("Server Dashboard", "live metrics · services · files", m.width)
	tabs := renderTabBar(m.tab, m.width)

	var body string
	switch m.tab {
	case tabServices:
		body = m.renderServices()
	case tabFiles:
		body = m.renderFiles()
	default:
		body = m.renderOverview()
	}

	panel := theme.PanelStyle().Width(layout.PanelWidth(m.width)).Render(body)

	quickText := " 1/2/3 tabs · d docker · p pm2 · l logs · m map · r refresh · b back "
	if m.tab == tabServices {
		quickText = " a filter · enter open · " + quickText
	}
	if m.tab == tabFiles {
		quickText = " enter open · - up · g path · . refresh · " + quickText
	}
	if layout.Breakpoint(m.width) == layout.BreakpointNarrow {
		quickText = " 1/2/3 · d · p · l · b "
	}
	quick := theme.MutedText().Render(quickText)

	help := helpForTab(m)
	help.Width = m.width

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		tabs,
		panel,
		quick,
		help.View(),
	)
}

func renderTabBar(active dashTab, width int) string {
	labels := []struct {
		tab dashTab
		txt string
	}{
		{tabOverview, "[1] Overview"},
		{tabServices, "[2] Services"},
		{tabFiles, "[3] Files"},
	}
	var parts []string
	for _, l := range labels {
		if l.tab == active {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Primary).Render(l.txt))
		} else {
			parts = append(parts, theme.MutedText().Render(l.txt))
		}
	}
	line := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	if width > 0 {
		line = lipgloss.NewStyle().Width(width).Render(line)
	}
	return line
}

func helpForTab(m *Model) components.HelpBar {
	switch m.tab {
	case tabServices:
		return components.NewHelpBar(
			components.KeyBinding{Key: "a", Desc: "filter"},
			components.KeyBinding{Key: "enter", Desc: "open"},
			components.KeyBinding{Key: "r", Desc: "refresh"},
			components.KeyBinding{Key: "1/2/3", Desc: "tabs"},
			components.KeyBinding{Key: "b", Desc: "back"},
		)
	case tabFiles:
		return components.NewHelpBar(
			components.KeyBinding{Key: "enter", Desc: "open"},
			components.KeyBinding{Key: "-", Desc: "up dir"},
			components.KeyBinding{Key: "g", Desc: "go path"},
			components.KeyBinding{Key: ".", Desc: "refresh"},
			components.KeyBinding{Key: "b", Desc: "back"},
		)
	default:
		return components.NewHelpBar(
			components.KeyBinding{Key: "r", Desc: "refresh"},
			components.KeyBinding{Key: "d/p/l", Desc: "docker/pm2/logs"},
			components.KeyBinding{Key: "1/2/3", Desc: "tabs"},
			components.KeyBinding{Key: "b", Desc: "back"},
		)
	}
}

func loadingText(msg string) string {
	return theme.MutedText().Render("  " + msg)
}

func errorText(msg string) string {
	return theme.ErrorText().Render("  " + msg)
}
