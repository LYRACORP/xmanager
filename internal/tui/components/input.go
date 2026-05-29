package components

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

func StyledTextInput(ti textinput.Model, width int) textinput.Model {
	ti.Width = layout.InputWidth(width)
	ti.PromptStyle = theme.InputStyle()
	ti.TextStyle = theme.InputStyle()
	ti.PlaceholderStyle = theme.MutedText()
	if ti.Focused() {
		ti.PromptStyle = theme.InputFocusedStyle()
		ti.TextStyle = theme.InputFocusedStyle()
	}
	return ti
}

func ApplyInputTheme(ti textinput.Model, width int, focused bool) textinput.Model {
	ti.Width = layout.InputWidth(width)
	if focused {
		ti.PromptStyle = theme.InputFocusedStyle()
		ti.TextStyle = theme.InputFocusedStyle()
	} else {
		ti.PromptStyle = theme.InputStyle()
		ti.TextStyle = theme.InputStyle()
	}
	ti.PlaceholderStyle = theme.MutedText()
	return ti
}

func RenderInputPanel(content string, width int, focused bool) string {
	style := theme.PanelStyle().Width(layout.PanelWidth(width))
	if focused {
		style = theme.ActivePanelStyle().Width(layout.PanelWidth(width))
	}
	return style.Render(content)
}

func RenderFormField(label, value string, width int, active bool) string {
	cursor := "  "
	if active {
		cursor = theme.KeyStyle().Render("> ")
	}
	line := cursor + label + " " + value
	style := theme.PanelStyle().Width(layout.PanelWidth(width))
	if active {
		style = theme.ActivePanelStyle().Width(layout.PanelWidth(width))
	}
	return style.Render(line)
}

func EmptyState(width int) string {
	return theme.PanelStyle().
		Width(layout.PanelWidth(width)).
		Render(theme.EmptyStateText())
}
