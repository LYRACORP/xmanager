package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gorm.io/gorm"

	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

const (
	roleUser      = "user"
	roleAssistant = "assistant"
	roleSystem    = "system"
)

// ChatMessage is one turn in the session; persisted as JSON in AISession.MessagesJSON.
type ChatMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type sessionLoadedMsg struct {
	session storage.AISession
	msgs    []ChatMessage
	err     error
}

type sessionSavedMsg struct {
	err error
}

// Model is the full-screen AI chat UI (responses simulated until provider wiring exists).
type Model struct {
	ctx *shared.AppContext

	width  int
	height int

	viewport viewport.Model
	input    textinput.Model

	session   storage.AISession
	sessionID uint
	messages  []ChatMessage

	focusInput bool
	status     string
}

func New(ctx *shared.AppContext) *Model {
	ti := textinput.New()
	ti.Placeholder = "Message…"
	ti.CharLimit = 8000
	ti.Width = 50

	m := &Model{
		ctx:        ctx,
		input:      ti,
		focusInput: true,
	}
	m.viewport.MouseWheelEnabled = true
	return m
}

func (m *Model) Name() string { return "AI Chat" }

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.layout()
}

func (m *Model) layout() {
	inputRows := 3
	header := 2
	help := 1
	vh := m.height - header - inputRows - help
	if vh < 5 {
		vh = 5
	}
	m.viewport.Width = max(10, m.width-2)
	m.viewport.Height = vh
	m.input.Width = max(20, m.width-6)
	m.refreshViewportContent()
}

func (m *Model) Init() tea.Cmd {
	m.layout()
	return m.loadSession
}

func (m *Model) loadSession() tea.Msg {
	if m.ctx == nil || m.ctx.DB == nil {
		return sessionLoadedMsg{err: errors.New("database not available")}
	}
	if m.ctx.ServerID == 0 {
		return sessionLoadedMsg{err: errors.New("no server selected")}
	}

	var sess storage.AISession
	err := m.ctx.DB.
		Where("server_id = ?", m.ctx.ServerID).
		Order("updated_at desc").
		First(&sess).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return sessionLoadedMsg{
				session: storage.AISession{
					ServerID: m.ctx.ServerID,
					Title:    "Chat",
				},
				msgs: nil,
				err:  nil,
			}
		}
		return sessionLoadedMsg{err: err}
	}

	msgs, decErr := decodeMessages(sess.MessagesJSON)
	if decErr != nil {
		return sessionLoadedMsg{session: sess, msgs: nil, err: decErr}
	}
	return sessionLoadedMsg{session: sess, msgs: msgs, err: nil}
}

func decodeMessages(raw string) ([]ChatMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var msgs []ChatMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

func encodeMessages(msgs []ChatMessage) (string, error) {
	b, err := json.Marshal(msgs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionLoadedMsg:
		if msg.err != nil {
			m.status = msg.err.Error()
			m.messages = nil
			m.session = storage.AISession{}
			m.sessionID = 0
			return m, nil
		}
		m.session = msg.session
		m.sessionID = msg.session.ID
		m.messages = msg.msgs
		if m.session.Title == "" {
			m.session.Title = "Chat"
		}
		m.status = ""
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case sessionSavedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("save: %v", msg.err)
		} else {
			m.status = ""
		}
		return m, nil

	case tea.KeyMsg:
		return m.updateKeys(msg)

	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	if m.focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) updateKeys(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return shared.GoBackMsg{} }

	case "tab":
		m.focusInput = !m.focusInput
		if m.focusInput {
			m.input.Focus()
			return m, textinput.Blink
		}
		m.input.Blur()
		return m, nil

	case "ctrl+l":
		m.messages = nil
		m.refreshViewportContent()
		return m, m.persistSession()

	case "enter":
		if !m.focusInput {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.SetValue("")
		now := time.Now().UTC()
		m.messages = append(m.messages, ChatMessage{Role: roleUser, Content: text, CreatedAt: now})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		reply := simulatedAssistantReply(m.ctx, text)
		m.messages = append(m.messages, ChatMessage{
			Role: roleAssistant, Content: reply, CreatedAt: time.Now().UTC(),
		})
		m.refreshViewportContent()
		m.viewport.GotoBottom()
		return m, m.persistSession()
	}

	if !m.focusInput {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	// Scroll chat while input focused
	switch msg.String() {
	case "pgup", "b":
		m.viewport.LineUp(5)
		return m, nil
	case "pgdown", "f":
		m.viewport.LineDown(5)
		return m, nil
	case "home", "g":
		m.viewport.GotoTop()
		return m, nil
	case "end", "G":
		m.viewport.GotoBottom()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func simulatedAssistantReply(ctx *shared.AppContext, userText string) string {
	provider := "ollama"
	model := "llama3"
	if ctx != nil && ctx.Config != nil {
		if ctx.Config.AI.Provider != "" {
			provider = ctx.Config.AI.Provider
		}
		if ctx.Config.AI.Model != "" {
			model = ctx.Config.AI.Model
		}
	}
	preview := userText
	if len(preview) > 200 {
		preview = preview[:200] + "…"
	}
	return fmt.Sprintf(
		"[Simulated · provider=%s model=%s]\n\nYou asked:\n%s\n\n"+
			"Real completions will use your configured provider and API key / Ollama host once AI integration is wired in.",
		provider,
		model,
		preview,
	)
}

func (m *Model) persistSession() tea.Cmd {
	return func() tea.Msg {
		if m.ctx == nil || m.ctx.DB == nil {
			return sessionSavedMsg{err: errors.New("database not available")}
		}
		if m.ctx.ServerID == 0 {
			return sessionSavedMsg{err: errors.New("no server selected")}
		}
		raw, err := encodeMessages(m.messages)
		if err != nil {
			return sessionSavedMsg{err: err}
		}
		m.session.ServerID = m.ctx.ServerID
		if m.session.Title == "" {
			m.session.Title = "Chat"
		}
		m.session.MessagesJSON = raw

		if m.sessionID == 0 {
			if err := m.ctx.DB.Create(&m.session).Error; err != nil {
				return sessionSavedMsg{err: err}
			}
			m.sessionID = m.session.ID
			return sessionSavedMsg{err: nil}
		}
		return sessionSavedMsg{err: m.ctx.DB.Save(&m.session).Error}
	}
}

func (m *Model) refreshViewportContent() {
	m.viewport.SetContent(m.renderMessages())
}

func (m *Model) renderMessages() string {
	if len(m.messages) == 0 {
		return theme.MutedText().Render("No messages yet. Type below and press Enter. Session is stored per server in the local database.")
	}
	var b strings.Builder
	for _, msg := range m.messages {
		b.WriteString(m.renderOneMessage(msg))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderOneMessage(msg ChatMessage) string {
	ts := msg.CreatedAt.Format("15:04:05")
	switch msg.Role {
	case roleUser:
		head := theme.TitleStyle().Render(fmt.Sprintf("You · %s", ts))
		body := lipgloss.NewStyle().Foreground(theme.Current.Text).Width(m.viewport.Width).Render(strings.TrimSpace(msg.Content))
		return lipgloss.JoinVertical(lipgloss.Left, head, body)
	case roleAssistant:
		head := theme.HeaderStyle().Render(fmt.Sprintf("Assistant · %s", ts))
		body := lipgloss.NewStyle().Foreground(theme.Current.TextDim).Width(m.viewport.Width).Render(strings.TrimSpace(msg.Content))
		return lipgloss.JoinVertical(lipgloss.Left, head, body)
	case roleSystem:
		head := theme.WarningText().Render(fmt.Sprintf("System · %s", ts))
		body := lipgloss.NewStyle().Foreground(theme.Current.Muted).Width(m.viewport.Width).Render(strings.TrimSpace(msg.Content))
		return lipgloss.JoinVertical(lipgloss.Left, head, body)
	default:
		head := theme.MutedText().Render(fmt.Sprintf("%s · %s", msg.Role, ts))
		body := lipgloss.NewStyle().Foreground(theme.Current.Text).Width(m.viewport.Width).Render(strings.TrimSpace(msg.Content))
		return lipgloss.JoinVertical(lipgloss.Left, head, body)
	}
}

func (m *Model) View() string {
	title := theme.HeaderStyle().Render("AI Chat")
	sub := theme.SubtitleStyle().Width(m.width).Render(m.subtitleLine())
	header := lipgloss.JoinVertical(lipgloss.Left, title, sub)

	vp := theme.PanelStyle().
		Width(m.width).
		Height(m.viewport.Height + 2).
		Render(m.viewport.View())

	inLabel := "Message"
	if m.focusInput {
		inLabel = theme.KeyStyle().Render("Message") + theme.DescStyle().Render(" (focused)")
	} else {
		inLabel = theme.MutedText().Render("Message (Tab to focus)")
	}
	inputBlock := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().PaddingLeft(1).Render(inLabel),
		lipgloss.NewStyle().PaddingLeft(1).Render(m.input.View()),
	)

	help := components.NewHelpBar(
		components.KeyBinding{Key: "enter", Desc: "send"},
		components.KeyBinding{Key: "tab", Desc: "focus"},
		components.KeyBinding{Key: "ctrl+l", Desc: "clear"},
		components.KeyBinding{Key: "pgup/pgdn", Desc: "scroll"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width

	status := ""
	if m.status != "" {
		status = "\n" + theme.ErrorText().Render("  "+m.status)
	}

	return lipgloss.JoinVertical(lipgloss.Left, header+status, vp, inputBlock, help.View())
}

func (m *Model) subtitleLine() string {
	sid := uint(0)
	if m.ctx != nil {
		sid = m.ctx.ServerID
	}
	parts := []string{fmt.Sprintf("server_id=%d", sid)}
	if m.ctx != nil && m.ctx.Config != nil {
		parts = append(parts,
			fmt.Sprintf("ai=%s/%s", m.ctx.Config.AI.Provider, m.ctx.Config.AI.Model),
		)
	}
	if m.sessionID != 0 {
		parts = append(parts, fmt.Sprintf("session#%d", m.sessionID))
	}
	return strings.Join(parts, " · ")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
