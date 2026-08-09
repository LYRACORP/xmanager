package components

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type ScreenFrame struct {
	Title       string
	Subtitle    string
	Width       int
	Body        string
	LocalChrome int
}

func (f ScreenFrame) View() string {
	title := theme.ScreenChrome(f.Title, f.Subtitle, f.Width)
	pw := layout.PanelWidth(f.Width)
	panel := theme.PanelStyle().
		Width(pw).
		MaxWidth(pw).
		Render(f.Body)
	return lipgloss.JoinVertical(lipgloss.Left, title, panel)
}

func (f ScreenFrame) BodyHeight(contentHeight, floor int) int {
	return layout.BodyHeight(contentHeight, f.LocalChrome, floor)
}

func FrameChromeRows(hasSubtitle bool) int {
	// title (+ subtitle) + panel top/bottom borders
	rows := 1 + 2
	if hasSubtitle {
		rows++
	}
	return rows
}
