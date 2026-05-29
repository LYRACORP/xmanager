package components

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type LoadingSpinner struct {
	spinner spinner.Model
	label   string
	active  bool
}

func NewLoadingSpinner(label string) LoadingSpinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(theme.Current.Accent)
	return LoadingSpinner{spinner: s, label: label, active: true}
}

func (l LoadingSpinner) Update(msg tea.Msg) (LoadingSpinner, tea.Cmd) {
	if !l.active {
		return l, nil
	}
	var cmd tea.Cmd
	l.spinner, cmd = l.spinner.Update(msg)
	return l, cmd
}

func (l LoadingSpinner) View() string {
	if !l.active {
		return ""
	}
	text := l.spinner.View()
	if l.label != "" {
		text += " " + theme.MutedText().Render(l.label)
	}
	return text
}

func (l LoadingSpinner) SetActive(active bool) LoadingSpinner {
	l.active = active
	return l
}

func (l LoadingSpinner) Tick() tea.Cmd {
	if !l.active {
		return nil
	}
	return l.spinner.Tick
}
