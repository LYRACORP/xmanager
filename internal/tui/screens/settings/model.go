package settings

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/activity"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type fieldIdx int

const (
	fieldAIProvider fieldIdx = iota
	fieldAIModel
	fieldAIKey
	fieldAIEndpoint
	fieldOllama
	fieldMaxLogLines
	fieldLogRetention
	fieldTGBot
	fieldTGChat
	fieldTGEnabled
	fieldUITheme
	fieldRefresh
	fieldWebEnabled
	fieldWebHost
	fieldWebPort
	fieldCount
)

type saveStatusMsg struct {
	text string
	err  bool
}

type Model struct {
	ctx         *shared.AppContext
	form        [fieldCount]textinput.Model
	focus       int
	width       int
	height      int
	status      string
	statusIsErr bool
	tgOn        bool
	webOn       bool
}

func New(ctx *shared.AppContext) *Model {
	m := &Model{ctx: ctx}
	m.initForm()
	m.loadFromConfig()
	return m
}

func (m *Model) initForm() {
	labels := [fieldCount]string{
		"AI provider", "AI model", "API key", "API endpoint (optional)", "Ollama host", "Max log lines",
		"Log retention (days)",
		"Telegram bot token", "Telegram chat ID", "Telegram enabled (space)",
		"UI theme (dark/light)", "UI refresh rate (s)",
		"Web panel enabled (space)", "Web host", "Web port",
	}
	for i := range m.form {
		ti := textinput.New()
		ti.Prompt = labels[i] + ": "
		ti.Width = 48
		if fieldIdx(i) == fieldAIKey || fieldIdx(i) == fieldTGBot {
			ti.EchoMode = textinput.EchoPassword
		}
		if fieldIdx(i) == fieldTGEnabled || fieldIdx(i) == fieldWebEnabled {
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
	m.form[fieldAIEndpoint].SetValue(c.AI.Endpoint)
	m.form[fieldOllama].SetValue(c.AI.OllamaHost)
	m.form[fieldMaxLogLines].SetValue(strconv.Itoa(c.AI.MaxLogLines))
	m.form[fieldLogRetention].SetValue(strconv.Itoa(c.Log.RetentionDays))
	m.form[fieldTGBot].SetValue(c.Telegram.BotToken)
	m.form[fieldTGChat].SetValue(c.Telegram.ChatID)
	m.tgOn = c.Telegram.Enabled
	m.syncTGEnabledField()
	m.form[fieldUITheme].SetValue(c.UI.Theme)
	m.form[fieldRefresh].SetValue(strconv.Itoa(c.UI.RefreshRate))
	m.webOn = c.Web.Enabled
	m.syncWebEnabledField()
	m.form[fieldWebHost].SetValue(c.Web.Host)
	m.form[fieldWebPort].SetValue(strconv.Itoa(c.Web.Port))
}

func (m *Model) syncTGEnabledField() {
	v := "off"
	if m.tgOn {
		v = "on"
	}
	m.form[fieldTGEnabled].SetValue(v)
}

func (m *Model) syncWebEnabledField() {
	v := "off"
	if m.webOn {
		v = "on"
	}
	m.form[fieldWebEnabled].SetValue(v)
}

func (m *Model) isToggle(i int) bool {
	return fieldIdx(i) == fieldTGEnabled || fieldIdx(i) == fieldWebEnabled
}

func (m *Model) Name() string { return "Settings" }

func (m *Model) KeyBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "tab", Desc: "field"},
		{Key: "space", Desc: "toggle"},
		{Key: "enter", Desc: "save"},
		{Key: "ctrl+s", Desc: "save"},
	}
}

func (m *Model) OnNavigate(_ map[string]interface{}) {}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	for i := range m.form {
		if !m.isToggle(i) {
			m.form[i].Width = layout.InputWidth(m.width)
		}
	}
}

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
	if !m.isToggle(i) {
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
			if !m.isToggle(m.focus) {
				return m, textinput.Blink
			}
			return m, nil
		case "shift+tab", "up":
			next := (m.focus - 1 + int(fieldCount)) % int(fieldCount)
			m.focusField(next)
			if !m.isToggle(m.focus) {
				return m, textinput.Blink
			}
			return m, nil
		case " ":
			if fieldIdx(m.focus) == fieldTGEnabled {
				m.tgOn = !m.tgOn
				m.syncTGEnabledField()
				return m, nil
			}
			if fieldIdx(m.focus) == fieldWebEnabled {
				m.webOn = !m.webOn
				m.syncWebEnabledField()
				return m, nil
			}
		case "enter":
			if m.focus < int(fieldCount)-1 {
				m.focusField(m.focus + 1)
				if !m.isToggle(m.focus) {
					return m, textinput.Blink
				}
				return m, nil
			}
			return m, m.save()
		}
		if !m.isToggle(m.focus) {
			var cmd tea.Cmd
			m.form[m.focus], cmd = m.form[m.focus].Update(msg)
			return m, cmd
		}
		return m, nil
	}
	if !m.isToggle(m.focus) {
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
	cfg.AI.Endpoint = strings.TrimSpace(m.form[fieldAIEndpoint].Value())
	cfg.AI.OllamaHost = strings.TrimSpace(m.form[fieldOllama].Value())
	if n, err := strconv.Atoi(strings.TrimSpace(m.form[fieldMaxLogLines].Value())); err == nil {
		cfg.AI.MaxLogLines = n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(m.form[fieldLogRetention].Value())); err == nil {
		cfg.Log.RetentionDays = config.ClampRetentionDays(n)
	}
	cfg.Telegram.BotToken = m.form[fieldTGBot].Value()
	cfg.Telegram.ChatID = strings.TrimSpace(m.form[fieldTGChat].Value())
	cfg.Telegram.Enabled = m.tgOn
	cfg.UI.Theme = strings.TrimSpace(m.form[fieldUITheme].Value())
	if n, err := strconv.Atoi(strings.TrimSpace(m.form[fieldRefresh].Value())); err == nil {
		cfg.UI.RefreshRate = n
	}
	cfg.Web.Enabled = m.webOn
	cfg.Web.Host = strings.TrimSpace(m.form[fieldWebHost].Value())
	if n, err := strconv.Atoi(strings.TrimSpace(m.form[fieldWebPort].Value())); err == nil {
		cfg.Web.Port = n
	}
	return config.Save(cfg)
}

func (m *Model) save() tea.Cmd {
	return func() tea.Msg {
		if err := m.applyFormToConfig(); err != nil {
			return saveStatusMsg{text: err.Error(), err: true}
		}
		activity.SetRetentionDays(m.ctx.Config.Log.RetentionDays)
		if m.ctx.DB != nil {
			activity.Trim(m.ctx.DB)
		}
		theme.SetTheme(m.ctx.Config.UI.Theme)
		return saveStatusMsg{text: "settings saved"}
	}
}

func (m *Model) View() string {
	var b strings.Builder
	title := theme.TitleStyle().Render("Settings")
	b.WriteString(title + "\n\n")

	for i := range m.form {
		line := m.form[i].View()
		if m.isToggle(i) {
			state := m.form[i].Value()
			label := "Telegram enabled"
			if fieldIdx(i) == fieldWebEnabled {
				label = "Web panel enabled"
			}
			line = label + ": " + state + theme.MutedText().Render("  (space toggles)")
			if m.focus == i {
				line = theme.KeyStyle().Render("> ") + line
			} else {
				line = "  " + line
			}
		} else if m.focus == i {
			line = theme.KeyStyle().Render("> ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}

	if m.status != "" {
		style := theme.SuccessText()
		if m.statusIsErr {
			style = theme.ErrorText()
		}
		b.WriteString("\n" + style.Render(m.status))
	}

	box := lipgloss.NewStyle().
		Width(m.width - 2).
		MaxHeight(m.height).
		Render(b.String())
	return box
}
