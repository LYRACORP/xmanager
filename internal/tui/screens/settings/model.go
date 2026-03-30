package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type fieldIdx int

const (
	fieldAIProvider fieldIdx = iota
	fieldAIModel
	fieldAIKey
	fieldOllama
	fieldMaxLogLines
	fieldTGBot
	fieldTGChat
	fieldTGEnabled
	fieldUITheme
	fieldRefresh
	fieldCount
)

type saveStatusMsg struct {
	text string
	err  bool
}

type Model struct {
	ctx     *shared.AppContext
	form    [fieldCount]textinput.Model
	focus   int
	width   int
	height     int
	status     string
	statusIsErr bool
	tgOn       bool
}

func New(ctx *shared.AppContext) *Model {
	m := &Model{ctx: ctx}
	m.initForm()
	m.loadFromConfig()
	return m
}

func (m *Model) initForm() {
	labels := [fieldCount]string{
		"AI provider", "AI model", "API key", "Ollama host", "Max log lines",
		"Telegram bot token", "Telegram chat ID", "Telegram enabled (space)",
		"UI theme (dark/light)", "UI refresh rate (s)",
	}
	for i := range m.form {
		ti := textinput.New()
		ti.Prompt = labels[i] + ": "
		ti.Width = 48
		if fieldIdx(i) == fieldAIKey || fieldIdx(i) == fieldTGBot {
			ti.EchoMode = textinput.EchoPassword
		}
		if fieldIdx(i) == fieldTGEnabled {
			ti.Blur()
		}
		m.form[i] = ti
	}
}

func (m *Model) loadFromConfig() {
	c := m.ctx.Config
	m.form[fieldAIProvider].SetValue(c.AI.Provider)
	m.form[fieldAIModel].SetValue(c.AI.Model)
	m.form[fieldAIKey].SetValue(c.AI.APIKey)
	m.form[fieldOllama].SetValue(c.AI.OllamaHost)
	m.form[fieldMaxLogLines].SetValue(strconv.Itoa(c.AI.MaxLogLines))
	m.form[fieldTGBot].SetValue(c.Telegram.BotToken)
	m.form[fieldTGChat].SetValue(c.Telegram.ChatID)
	m.tgOn = c.Telegram.Enabled
	m.syncTGEnabledField()
	m.form[fieldUITheme].SetValue(c.UI.Theme)
	m.form[fieldRefresh].SetValue(strconv.Itoa(c.UI.RefreshRate))
}

func (m *Model) syncTGEnabledField() {
	v := "off"
	if m.tgOn {
		v = "on"
	}
	m.form[fieldTGEnabled].SetValue(v)
}

func (m *Model) Name() string     { return "Settings" }
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m *Model) Init() tea.Cmd {
	m.loadFromConfig()
	m.focusField(0)
	return textinput.Blink
}

func (m *Model) focusField(i int) {
	for j := range m.form {
		m.form[j].Blur()
	}
	m.focus = i
	if fieldIdx(i) != fieldTGEnabled {
		m.form[i].Focus()
	}
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case saveStatusMsg:
		m.status = msg.text
		m.statusIsErr = msg.err
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "ctrl+s":
			return m, m.save()
		case "tab", "down":
			next := (m.focus + 1) % int(fieldCount)
			m.focusField(next)
			if fieldIdx(m.focus) != fieldTGEnabled {
				return m, textinput.Blink
			}
			return m, nil
		case "shift+tab", "up":
			next := (m.focus - 1 + int(fieldCount)) % int(fieldCount)
			m.focusField(next)
			if fieldIdx(m.focus) != fieldTGEnabled {
				return m, textinput.Blink
			}
			return m, nil
		case " ":
			if fieldIdx(m.focus) == fieldTGEnabled {
				m.tgOn = !m.tgOn
				m.syncTGEnabledField()
				return m, nil
			}
		case "enter":
			if m.focus < int(fieldCount)-1 {
				m.focusField(m.focus + 1)
				if fieldIdx(m.focus) != fieldTGEnabled {
					return m, textinput.Blink
				}
				return m, nil
			}
			return m, m.save()
		}
		if fieldIdx(m.focus) != fieldTGEnabled {
			var cmd tea.Cmd
			m.form[m.focus], cmd = m.form[m.focus].Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if fieldIdx(m.focus) != fieldTGEnabled {
		var cmd tea.Cmd
		m.form[m.focus], cmd = m.form[m.focus].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) applyFormToConfig() error {
	cfg := m.ctx.Config
	cfg.AI.Provider = strings.TrimSpace(m.form[fieldAIProvider].Value())
	cfg.AI.Model = strings.TrimSpace(m.form[fieldAIModel].Value())
	cfg.AI.APIKey = m.form[fieldAIKey].Value()
	cfg.AI.OllamaHost = strings.TrimSpace(m.form[fieldOllama].Value())
	n, err := strconv.Atoi(strings.TrimSpace(m.form[fieldMaxLogLines].Value()))
	if err != nil || n < 1 {
		return fmt.Errorf("max log lines must be a positive integer")
	}
	cfg.AI.MaxLogLines = n
	cfg.Telegram.BotToken = m.form[fieldTGBot].Value()
	cfg.Telegram.ChatID = strings.TrimSpace(m.form[fieldTGChat].Value())
	cfg.Telegram.Enabled = m.tgOn
	themeName := strings.TrimSpace(strings.ToLower(m.form[fieldUITheme].Value()))
	if themeName != "light" && themeName != "dark" {
		return fmt.Errorf("theme must be dark or light")
	}
	cfg.UI.Theme = themeName
	r, err := strconv.Atoi(strings.TrimSpace(m.form[fieldRefresh].Value()))
	if err != nil || r < 1 {
		return fmt.Errorf("refresh rate must be a positive integer (seconds)")
	}
	cfg.UI.RefreshRate = r
	return nil
}

func (m *Model) save() tea.Cmd {
	return func() tea.Msg {
		if err := m.applyFormToConfig(); err != nil {
			return saveStatusMsg{text: err.Error(), err: true}
		}
		if err := config.Save(m.ctx.Config); err != nil {
			return saveStatusMsg{text: err.Error(), err: true}
		}
		theme.SetTheme(m.ctx.Config.UI.Theme)
		return saveStatusMsg{text: "Saved.", err: false}
	}
}

func (m *Model) View() string {
	header := theme.HeaderStyle().Render("Settings")
	var b strings.Builder
	for i := range m.form {
		cursor := "  "
		if i == m.focus {
			cursor = theme.KeyStyle().Render("> ")
		}
		fi := fieldIdx(i)
		if fi == fieldTGEnabled {
			state := theme.MutedText().Render("off")
			if m.tgOn {
				state = theme.SuccessText().Render("on")
			}
			b.WriteString(cursor)
			b.WriteString("Telegram enabled: ")
			b.WriteString(state)
			b.WriteString(theme.MutedText().Render("  (space toggles)"))
			b.WriteByte('\n')
			continue
		}
		b.WriteString(cursor)
		b.WriteString(m.form[i].View())
		b.WriteByte('\n')
	}
	status := ""
	if m.status != "" {
		if m.statusIsErr {
			status = "\n " + theme.ErrorText().Render(m.status)
		} else {
			status = "\n " + theme.SuccessText().Render(m.status)
		}
	}
	help := components.NewHelpBar(
		components.KeyBinding{Key: "tab", Desc: "field"},
		components.KeyBinding{Key: "enter", Desc: "save"},
		components.KeyBinding{Key: "ctrl+s", Desc: "save"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width
	footer := theme.MutedText().Render("  API key and bot token are hidden while typing.")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", b.String(), status, "", footer, help.View())
}

var _ shared.Screen = (*Model)(nil)
