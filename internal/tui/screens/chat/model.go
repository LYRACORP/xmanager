package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gorm.io/gorm"

	"github.com/lyracorp/xmanager/internal/ai"
	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/recon"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
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

type replyMsg struct {
	text string
	err  error
}

// Model is the full-screen AI chat UI.
type Model struct {
	ctx *shared.AppContext

	width  int
	height int

	scroll components.ScrollView
	input  textinput.Model

	session   storage.AISession
	sessionID uint
	messages  []ChatMessage

	focusInput bool
	status     string
	busy       bool
	agent      *ai.Agent
}

func New(ctx *shared.AppContext) *Model {
	ti := textinput.New()
	ti.Placeholder = "Message…"
	ti.CharLimit = 8000
	ti.Width = 50

	m := &Model{
		ctx:        ctx,
		scroll:     components.NewScrollView(50, 10),
		input:      ti,
		focusInput: true,
	}
	return m
}

func (m *Model) Name() string { return "AI Chat" }

func (m *Model) KeyBindings() []components.KeyBinding {
	return []components.KeyBinding{
		{Key: "enter", Desc: "send"},
		{Key: "tab", Desc: "focus"},
		{Key: "ctrl+l", Desc: "clear"},
		{Key: "pgup/pgdn", Desc: "scroll"},
	}
}

func (m *Model) OnNavigate(params map[string]interface{}) {
	if params == nil {
		return
	}
	raw, ok := params["prefill"]
	if !ok {
		return
	}
	text, ok := raw.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return
	}
	m.input.SetValue(text)
	m.focusInput = true
	m.input.Focus()
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.layout()
}

func (m *Model) layout() {
	scrollH := layout.TableHeight(m.height, 6, 5)
	scrollW := layout.Clamp(10, m.width-4, m.width-2)
	m.scroll = m.scroll.SetSize(scrollW, scrollH)
	m.input = components.ApplyInputTheme(m.input, m.width, m.focusInput)
	m.syncScrollContent()
}

func (m *Model) Init() tea.Cmd {
	m.layout()
	return m.loadSession
}

func (m *Model) loadSession() tea.Msg {
	if m.ctx == nil || m.ctx.DB == nil {
		return sessionLoadedMsg{err: errors.New("database not available")}
	}

	q := m.ctx.DB.Where("server_id = ?", m.ctx.ServerID).Order("updated_at desc")

	var sess storage.AISession
	err := q.First(&sess).Error

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
		m.syncScrollContent()
		m.scroll = m.scroll.GotoBottom()
		return m, m.scroll.ScheduleFlush()

	case sessionSavedMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf("save: %v", msg.err)
		} else {
			m.status = ""
		}
		return m, nil

	case replyMsg:
		m.busy = false
		if msg.err != nil {
			m.status = msg.err.Error()
			m.messages = append(m.messages, ChatMessage{Role: roleAssistant, Content: "Error: " + msg.err.Error(), CreatedAt: time.Now().UTC()})
		} else {
			m.status = ""
			m.messages = append(m.messages, ChatMessage{Role: roleAssistant, Content: msg.text, CreatedAt: time.Now().UTC()})
		}
		m.syncScrollContent()
		m.scroll = m.scroll.GotoBottom()
		return m, tea.Batch(m.scroll.ScheduleFlush(), m.persistSession())

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}

	if m.focusInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.scroll, cmd = m.scroll.Update(msg)
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
		m.agent = nil
		m.syncScrollContent()
		return m, tea.Batch(m.scroll.ScheduleFlush(), m.persistSession())

	case "enter":
		if !m.focusInput || m.busy {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.SetValue("")
		now := time.Now().UTC()
		m.messages = append(m.messages, ChatMessage{Role: roleUser, Content: text, CreatedAt: now})
		m.busy = true
		m.status = "thinking…"
		m.syncScrollContent()
		m.scroll = m.scroll.GotoBottom()
		if err := m.ensureAgent(); err != nil {
			m.busy = false
			m.status = err.Error()
			return m, nil
		}
		return m, tea.Batch(m.scroll.ScheduleFlush(), m.send(text))
	}

	if !m.focusInput {
		var cmd tea.Cmd
		m.scroll, cmd = m.scroll.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case "pgup", "b":
		var cmd tea.Cmd
		m.scroll, cmd = m.scroll.Update(tea.KeyMsg{Type: tea.KeyPgUp})
		return m, cmd
	case "pgdown", "f":
		var cmd tea.Cmd
		m.scroll, cmd = m.scroll.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		return m, cmd
	case "home", "g":
		m.scroll = m.scroll.SetYOffset(0)
		return m, nil
	case "end", "G":
		m.scroll = m.scroll.GotoBottom()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) send(userText string) tea.Cmd {
	agent := m.agent
	wf := m.ctx.Workflows
	return func() tea.Msg {
		if wf != nil {
			wf.TriggerChat(userText)
		}
		text, err := agent.Send(nil, userText)
		return replyMsg{text: text, err: err}
	}
}

func (m *Model) ensureAgent() error {
	if m.agent != nil {
		return nil
	}
	if m.ctx == nil || m.ctx.Config == nil {
		return errors.New("config not available")
	}
	p, err := ai.NewProvider(ai.ProviderConfigFromAI(m.ctx.Config.AI))
	if err != nil {
		return err
	}
	cat := m.ctx.Catalog
	if cat == nil {
		cat = ops.New(m.ctx.DB, m.ctx.Pool)
	}
	extra := ""
	if m.ctx.ServerID > 0 {
		sctx := ai.ServerContext{ServerName: fmt.Sprintf("id=%d", m.ctx.ServerID)}
		if prof, err := recon.GetLatestProfile(m.ctx.DB, m.ctx.ServerID); err == nil && prof != nil {
			sctx.Profile = prof.ProfileJSON
		}
		extra = ai.BuildSystemPrompt(sctx).Content
	}
	m.agent = ai.NewAgent(p, cat, extra)
	if m.ctx.ServerID > 0 {
		m.agent.SetLockedServer(m.ctx.ServerID)
	}
	if len(m.messages) > 0 {
		hist := make([]ai.Message, 0, len(m.messages))
		for _, msg := range m.messages {
			hist = append(hist, ai.Message{Role: ai.Role(msg.Role), Content: msg.Content})
		}
		m.agent.LoadHistory(hist)
	}
	return nil
}

func (m *Model) persistSession() tea.Cmd {
	return func() tea.Msg {
		if m.ctx == nil || m.ctx.DB == nil {
			return sessionSavedMsg{err: errors.New("database not available")}
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

func (m *Model) syncScrollContent() {
	m.scroll = m.scroll.SetContent(m.renderMessages())
}

func (m *Model) renderMessages() string {
	if len(m.messages) == 0 {
		return theme.EmptyStateText()
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
	content := strings.TrimSpace(msg.Content)
	width := m.scroll.Inner().Width
	if width < 10 {
		width = layout.Clamp(10, m.width-4, m.width-2)
	}
	bodyStyle := theme.MessageStyle(msg.Role).Width(width)

	switch msg.Role {
	case roleUser:
		head := theme.InfoBadge().Render("YOU") + " " + theme.MutedText().Render(ts)
		return lipgloss.JoinVertical(lipgloss.Left, head, bodyStyle.Render(content))
	case roleAssistant:
		head := theme.SuccessBadge().Render("AI") + " " + theme.MutedText().Render(ts)
		return lipgloss.JoinVertical(lipgloss.Left, head, bodyStyle.Render(content))
	case roleSystem:
		head := theme.WarningBadge().Render("SYS") + " " + theme.MutedText().Render(ts)
		return lipgloss.JoinVertical(lipgloss.Left, head, bodyStyle.Render(content))
	default:
		head := theme.MutedText().Render(fmt.Sprintf("%s · %s", msg.Role, ts))
		return lipgloss.JoinVertical(lipgloss.Left, head, bodyStyle.Render(content))
	}
}

func (m *Model) View() string {
	title := theme.ScreenChrome("AI Chat", m.subtitleLine(), m.width)

	vp := m.scroll.View()

	inLabel := theme.MutedText().Render("Message (Tab to focus)")
	if m.focusInput {
		inLabel = theme.KeyStyle().Render("Message") + theme.DescStyle().Render(" (focused)")
	}
	inputView := components.ApplyInputTheme(m.input, m.width, m.focusInput).View()
	inputBlock := components.RenderInputPanel(
		lipgloss.JoinVertical(lipgloss.Left, inLabel, inputView),
		m.width, m.focusInput,
	)

	status := ""
	if m.status != "" {
		status = "\n" + theme.ErrorText().Render("  "+m.status)
	}

	return lipgloss.JoinVertical(lipgloss.Left, title+status, vp, inputBlock)
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
	if m.agent != nil && m.agent.Pending() != nil {
		parts = append(parts, "type y to confirm destructive tool")
	}
	return strings.Join(parts, " · ")
}
