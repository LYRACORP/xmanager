package shared

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/localnet"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

// WebPanelProgressMsg is emitted during install/upgrade/uninstall.
type WebPanelProgressMsg struct {
	Pct    float64
	Detail string
}

// WebPanelDoneMsg is the final result of a web panel operation.
type WebPanelDoneMsg struct {
	ServerID  uint
	Action    string
	Installed bool
	URL       string
	Err       error
}

// ProgressNetTickMsg triggers a local network traffic sample while progress is active.
type ProgressNetTickMsg struct{}

// WebPanelProgressState tracks live progress for the TUI.
type WebPanelProgressState struct {
	Active bool
	Action string
	Pct    float64
	Detail string
	Log    []string

	NetIface   string
	NetRxBps   float64
	NetTxBps   float64
	NetRxTotal uint64
	NetTxTotal uint64
	netPrev    localnet.Counters
	netStart   localnet.Counters
	netReady   bool
}

const webPanelLogMax = 8

func (p *WebPanelProgressState) Start(action string) {
	*p = WebPanelProgressState{Active: true, Action: action, Pct: 0}
	if c, err := localnet.Sample(); err == nil {
		p.netPrev = c
		p.netStart = c
		p.NetIface = c.Iface
		p.netReady = true
	}
}

func (p *WebPanelProgressState) Apply(msg WebPanelProgressMsg) {
	p.Pct = msg.Pct
	p.Detail = msg.Detail
	if msg.Detail == "" {
		return
	}
	if n := len(p.Log); n > 0 && p.Log[n-1] == msg.Detail {
		return
	}
	p.Log = append(p.Log, msg.Detail)
	if len(p.Log) > webPanelLogMax {
		p.Log = p.Log[len(p.Log)-webPanelLogMax:]
	}
}

// SampleNet refreshes live send/receive rates from the local machine.
func (p *WebPanelProgressState) SampleNet() {
	if !p.Active {
		return
	}
	cur, err := localnet.Sample()
	if err != nil {
		return
	}
	if p.netReady {
		p.NetRxBps, p.NetTxBps = localnet.Rates(p.netPrev, cur)
		if cur.Rx >= p.netStart.Rx {
			p.NetRxTotal = cur.Rx - p.netStart.Rx
		}
		if cur.Tx >= p.netStart.Tx {
			p.NetTxTotal = cur.Tx - p.netStart.Tx
		}
	} else {
		p.netStart = cur
		p.netReady = true
	}
	p.netPrev = cur
	p.NetIface = cur.Iface
}

func (p *WebPanelProgressState) Reset() {
	*p = WebPanelProgressState{}
}

// TickProgressNet schedules the next network sample (~2 Hz).
func TickProgressNet() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return ProgressNetTickMsg{}
	})
}

// View renders a progress bar, live net traffic, and recent detail lines.
func (p WebPanelProgressState) View(width int) string {
	if !p.Active {
		return ""
	}
	title := p.Action
	if title == "" {
		title = "Working…"
	}
	switch p.Action {
	case "upgrade":
		title = "Upgrading web panel"
	case "install":
		title = "Installing web panel"
	case "uninstall":
		title = "Uninstalling web panel"
	}
	gW := width - 8
	if gW < 20 {
		gW = 20
	}
	if gW > 56 {
		gW = 56
	}
	g := components.Gauge{Label: "PROG", Value: p.Pct, Width: gW, ShowPct: true}
	lines := []string{
		" " + theme.TitleStyle().Render(title),
		" " + g.View(),
		" " + p.netLine(),
	}
	if p.Detail != "" {
		lines = append(lines, " "+theme.WarningText().Render(p.Detail))
	}
	for _, d := range p.Log {
		if d == p.Detail {
			continue
		}
		lines = append(lines, " "+theme.MutedText().Render("• "+d))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p WebPanelProgressState) netLine() string {
	iface := p.NetIface
	if iface == "" {
		iface = "?"
	}
	line := fmt.Sprintf("NET  ↓ %s  ↑ %s  (%s)  · session ↓%s ↑%s",
		localnet.FormatRate(p.NetRxBps),
		localnet.FormatRate(p.NetTxBps),
		iface,
		localnet.FormatBytes(p.NetRxTotal),
		localnet.FormatBytes(p.NetTxTotal),
	)
	style := theme.MutedText()
	// Highlight when there is meaningful traffic (not stuck idle).
	if p.NetRxBps+p.NetTxBps >= 1024 {
		style = theme.SuccessText()
	} else if p.netReady {
		style = theme.WarningText()
	}
	return style.Render(line)
}

// WaitMsg reads the next message from a progress channel (nil when closed).
func WaitMsg(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}
