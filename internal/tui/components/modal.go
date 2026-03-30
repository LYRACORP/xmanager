package components

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type ModalType int

const (
	ModalConfirm ModalType = iota
	ModalInput
	ModalInfo
)

type Modal struct {
	Type    ModalType
	Title   string
	Message string
	Input   textinput.Model
	Active  bool
	Width   int
	Height  int
}

func NewConfirmModal(title, message string) Modal {
	return Modal{
		Type:    ModalConfirm,
		Title:   title,
		Message: message,
		Active:  true,
	}
}

func NewInputModal(title, placeholder string) Modal {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	return Modal{
		Type:   ModalInput,
		Title:  title,
		Input:  ti,
		Active: true,
	}
}

func NewInfoModal(title, message string) Modal {
	return Modal{
		Type:    ModalInfo,
		Title:   title,
		Message: message,
		Active:  true,
	}
}

func (m Modal) View() string {
	if !m.Active {
		return ""
	}

	width := 50
	if m.Width > 0 {
		width = m.Width
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(theme.Current.Primary).
		Width(width - 4).
		Align(lipgloss.Center)

	contentStyle := lipgloss.NewStyle().
		Foreground(theme.Current.Text).
		Width(width - 4).
		Align(lipgloss.Center)

	var content string
	switch m.Type {
	case ModalConfirm:
		content = contentStyle.Render(m.Message) + "\n\n" +
			theme.KeyStyle().Render("y") + theme.DescStyle().Render(" confirm  ") +
			theme.KeyStyle().Render("n") + theme.DescStyle().Render(" cancel")
	case ModalInput:
		content = m.Input.View()
	case ModalInfo:
		content = contentStyle.Render(m.Message) + "\n\n" +
			theme.KeyStyle().Render("Enter") + theme.DescStyle().Render(" close")
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Current.Primary).
		Padding(1, 2).
		Width(width).
		Align(lipgloss.Center)

	return box.Render(
		titleStyle.Render(m.Title) + "\n\n" + content,
	)
}
