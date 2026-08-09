package dashboard

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

func renderDashboard(m *Model) string {
	tabs := components.NewTabBar([]components.TabItem{
		{ID: int(tabOverview), Label: "[1] Overview"},
		{ID: int(tabServices), Label: "[2] Services"},
		{ID: int(tabFiles), Label: "[3] Files"},
	}, int(m.tab))
	tabs.Width = m.width

	var body string
	switch m.tab {
	case tabServices:
		body = m.renderServices()
	case tabFiles:
		body = m.renderFiles()
	default:
		body = m.renderOverview()
	}

	if m.statusMsg != "" {
		style := theme.WarningText()
		if m.webBusy {
			style = theme.MutedText()
		}
		inner := layout.ContentWidth(m.width)
		wrapped := components.Wrap(m.statusMsg, inner)
		var styled []string
		for _, line := range strings.Split(wrapped, "\n") {
			styled = append(styled, " "+style.Render(line))
		}
		body = lipgloss.JoinVertical(lipgloss.Left, append([]string{body, ""}, styled...)...)
	}

	sub := "live metrics · services · files"
	if m.webInstalled {
		sub += " · web panel on"
	}

	frame := components.ScreenFrame{
		Title:       "Server Dashboard",
		Subtitle:    sub,
		Width:       m.width,
		LocalChrome: components.FrameChromeRows(true) + components.TabBarRows(),
		Body:        tabs.View() + "\n" + body,
	}
	return frame.View()
}

func loadingText(msg string) string {
	return theme.MutedText().Render("  " + msg)
}

func errorText(msg string) string {
	return theme.ErrorText().Render("  " + components.Truncate(msg, 80))
}
