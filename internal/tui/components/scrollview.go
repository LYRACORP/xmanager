package components

import (
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type scrollTickMsg struct{}

type ScrollView struct {
	viewport      viewport.Model
	content       string
	pending       string
	width         int
	height        int
	debounce      time.Duration
	autoScroll    bool
	contentDirty  bool
}

func NewScrollView(width, height int) ScrollView {
	vp := viewport.New(width, height)
	vp.Style = theme.ViewportStyle()
	return ScrollView{
		viewport:   vp,
		width:      width,
		height:     height,
		debounce:   33 * time.Millisecond,
		autoScroll: true,
	}
}

func (s ScrollView) SetSize(width, height int) ScrollView {
	s.width = width
	s.height = height
	s.viewport.Width = width
	s.viewport.Height = height
	return s
}

func (s ScrollView) SetContent(content string) ScrollView {
	if content == s.content {
		return s
	}
	s.pending = content
	s.contentDirty = true
	return s
}

func (s ScrollView) flush() ScrollView {
	if !s.contentDirty {
		return s
	}
	atBottom := s.viewport.AtBottom()
	s.content = s.pending
	s.viewport.SetContent(s.content)
	if s.autoScroll && atBottom {
		s.viewport.GotoBottom()
	}
	s.contentDirty = false
	s.pending = ""
	return s
}

func (s ScrollView) Update(msg tea.Msg) (ScrollView, tea.Cmd) {
	switch msg.(type) {
	case scrollTickMsg:
		s = s.flush()
		return s, nil
	}

	var cmd tea.Cmd
	s.viewport, cmd = s.viewport.Update(msg)
	return s, cmd
}

func (s ScrollView) View() string {
	if s.contentDirty {
		s = s.flush()
	}
	return s.viewport.View()
}

func (s ScrollView) ScheduleFlush() tea.Cmd {
	return tea.Tick(s.debounce, func(time.Time) tea.Msg {
		return scrollTickMsg{}
	})
}

func (s ScrollView) GotoBottom() ScrollView {
	s.viewport.GotoBottom()
	return s
}

func (s ScrollView) SetYOffset(y int) ScrollView {
	s.viewport.SetYOffset(y)
	return s
}

func (s ScrollView) YOffset() int {
	return s.viewport.YOffset
}

func (s ScrollView) AtBottom() bool {
	return s.viewport.AtBottom()
}

func (s ScrollView) Inner() viewport.Model {
	return s.viewport
}
