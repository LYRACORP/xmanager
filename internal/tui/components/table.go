package components

import (
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

func StyledTable(columns []table.Column, rows []table.Row, height int) table.Model {
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(height),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.Current.Border).
		BorderBottom(true).
		Bold(true).
		Foreground(theme.Current.Primary)

	s.Selected = s.Selected.
		Foreground(theme.Current.Text).
		Background(theme.Current.Surface).
		Bold(false)

	t.SetStyles(s)
	return t
}
