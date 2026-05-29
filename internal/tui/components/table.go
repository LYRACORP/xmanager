package components

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type ListTable struct {
	inner   table.Model
	cursor  int
	focused bool
	width   int
	height  int
	empty   bool
}

func NewListTable(columns []table.Column, rows []table.Row, height int) ListTable {
	return NewListTableFocused(columns, rows, height, true)
}

func NewListTableFocused(columns []table.Column, rows []table.Row, height int, focused bool) ListTable {
	t := newInnerTable(columns, rows, height, focused)
	return ListTable{
		inner:   t,
		cursor:  0,
		focused: focused,
		height:  height,
		empty:   len(rows) == 0,
	}
}

func newInnerTable(columns []table.Column, rows []table.Row, height int, focused bool) table.Model {
	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(focused),
		table.WithHeight(height),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(theme.Current.Border).
		BorderBottom(true).
		Bold(false).
		Foreground(theme.Current.TextDim)

	s.Selected = theme.SelectedRowStyle()

	s.Cell = s.Cell.
		Foreground(theme.Current.Text)

	t.SetStyles(s)
	return t
}

func (t ListTable) SetData(width int, columns []table.Column, rows []table.Row, height int) ListTable {
	cols := layout.AdaptiveColumns(width, columns)
	cursor := t.cursor
	if len(rows) == 0 {
		cursor = 0
	} else if cursor >= len(rows) {
		cursor = len(rows) - 1
	}

	if t.height == 0 {
		nt := NewListTableFocused(cols, rows, height, t.focused)
		if cursor > 0 && cursor < len(rows) {
			nt.inner.SetCursor(cursor)
			nt.cursor = cursor
		}
		nt.width = width
		return nt
	}

	t.inner.SetColumns(cols)
	t.inner.SetRows(rows)
	t.inner.SetHeight(height)
	t.inner.SetCursor(cursor)
	t.cursor = cursor
	t.height = height
	t.width = width
	t.empty = len(rows) == 0
	if t.focused {
		t.inner.Focus()
	} else {
		t.inner.Blur()
	}
	return t
}

func (t ListTable) SetSize(width, height int) ListTable {
	t.width = width
	t.height = height
	t.inner.SetHeight(height)
	return t
}

func (t ListTable) SetFocused(focused bool) ListTable {
	t.focused = focused
	if focused {
		t.inner.Focus()
	} else {
		t.inner.Blur()
	}
	return t
}

func (t ListTable) Update(msg tea.Msg) (ListTable, tea.Cmd) {
	var cmd tea.Cmd
	t.inner, cmd = t.inner.Update(msg)
	t.cursor = t.inner.Cursor()
	return t, cmd
}

func (t ListTable) View() string {
	if t.empty {
		return theme.PanelStyle().
			Width(layout.PanelWidth(t.width)).
			Height(t.height).
			Render(lipgloss.PlaceHorizontal(t.width, lipgloss.Center, theme.EmptyStateText()))
	}
	return t.inner.View()
}

func (t ListTable) Cursor() int {
	return t.cursor
}

func (t ListTable) SelectedRow() table.Row {
	rows := t.inner.Rows()
	if t.cursor < 0 || t.cursor >= len(rows) {
		return nil
	}
	return rows[t.cursor]
}

func (t ListTable) Rows() []table.Row {
	return t.inner.Rows()
}

func (t ListTable) Height() int {
	return t.height
}

// StyledTable preserves backward compatibility during migration.
func StyledTable(columns []table.Column, rows []table.Row, height int) table.Model {
	return newInnerTable(columns, rows, height, true)
}

func TableEmptyMessage() string {
	return theme.EmptyStateText()
}
