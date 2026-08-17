package docker

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	xmdocker "github.com/lyracorp/xmanager/internal/docker"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type swarmMode int

const (
	swarmModeList swarmMode = iota
	swarmModeJoinPick
	swarmModeConfirmLeave
	swarmModeConfirmRemove
)

type fleetJoinTarget struct {
	ID   uint
	Name string
	Host string
}

func (m *Model) loadSwarm(ex *ssh.Executor, tab viewTab) tea.Msg {
	mgr := xmdocker.NewManager(ex)
	info := mgr.SwarmInfo()
	msg := loadDoneMsg{tab: tab, swarm: info, brief: "swarm " + info.State}
	if !info.Manager() {
		return msg
	}
	nodes, err := mgr.ListNodes()
	if err != nil {
		return loadDoneMsg{tab: tab, err: err.Error(), swarm: info}
	}
	msg.swarmNodes = nodes
	msg.brief = fmt.Sprintf("%d nodes", len(nodes))
	if cmd, err := mgr.JoinCommand(xmdocker.RoleWorker); err == nil {
		msg.workerJoin = cmd
	}
	if cmd, err := mgr.JoinCommand(xmdocker.RoleManager); err == nil {
		msg.managerJoin = cmd
	}
	svcs, _ := mgr.ListServices()
	msg.swarmSvcs = svcs
	return msg
}

func (m *Model) copyWorkerJoin() tea.Cmd {
	if m.tab != tabSwarm || m.workerJoin == "" {
		return nil
	}
	if err := clipboard.WriteAll(m.workerJoin); err != nil {
		m.status = m.workerJoin
		return nil
	}
	m.status = "Copied worker join command"
	return nil
}

func (m *Model) swarmInit() tea.Cmd {
	if m.tab != tabSwarm {
		return nil
	}
	m.busy = true
	m.status = "Initializing swarm…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		if err := xmdocker.NewManager(ex).InitSwarm(""); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: "Swarm initialized"}
	}
}

func (m *Model) swarmLeave() tea.Cmd {
	m.busy = true
	m.status = "Leaving swarm…"
	m.swarmUI = swarmModeList
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		if err := xmdocker.NewManager(ex).LeaveSwarm(true); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: "Left swarm"}
	}
}

func (m *Model) swarmPromote() tea.Cmd {
	n, ok := m.selectedSwarmNode()
	if !ok {
		return nil
	}
	m.busy = true
	m.status = "Promoting " + n.Hostname + "…"
	sid := m.ctx.ServerID
	id := n.ID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		if err := xmdocker.NewManager(ex).Promote(id); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: "Promoted " + id}
	}
}

func (m *Model) swarmDemote() tea.Cmd {
	n, ok := m.selectedSwarmNode()
	if !ok {
		return nil
	}
	m.busy = true
	m.status = "Demoting " + n.Hostname + "…"
	sid := m.ctx.ServerID
	id := n.ID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		if err := xmdocker.NewManager(ex).Demote(id); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: "Demoted " + id}
	}
}

func (m *Model) swarmCycleAvailability() tea.Cmd {
	n, ok := m.selectedSwarmNode()
	if !ok {
		return nil
	}
	next := xmdocker.NextAvailability(n.Availability)
	m.busy = true
	m.status = "Availability " + next + "…"
	sid := m.ctx.ServerID
	id := n.ID
	host := n.Hostname
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		if err := xmdocker.NewManager(ex).SetAvailability(id, next); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: host + " → " + next}
	}
}

func (m *Model) swarmRemove() tea.Cmd {
	id := m.pendingNodeID
	m.pendingNodeID = ""
	m.swarmUI = swarmModeList
	if id == "" {
		return nil
	}
	m.busy = true
	m.status = "Removing node…"
	sid := m.ctx.ServerID
	return func() tea.Msg {
		ex, ok := m.ctx.Pool.GetExecutor(sid)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		if err := xmdocker.NewManager(ex).RemoveNode(id, true); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: "Removed node " + id}
	}
}

func (m *Model) openJoinPicker() tea.Cmd {
	if m.tab != tabSwarm || !m.swarm.Manager() {
		return nil
	}
	targets := m.fleetJoinTargets()
	if len(targets) == 0 {
		m.status = "No other connected fleet servers. Paste the join command on a host that has Docker."
		return nil
	}
	m.joinTargets = targets
	m.joinCursor = 0
	m.swarmUI = swarmModeJoinPick
	m.status = "Pick a connected server, then Enter to run docker swarm join"
	return nil
}

func (m *Model) fleetJoinTargets() []fleetJoinTarget {
	if m.ctx == nil || m.ctx.Pool == nil {
		return nil
	}
	var out []fleetJoinTarget
	for _, id := range m.ctx.Pool.ActiveConnections() {
		if id == m.ctx.ServerID {
			continue
		}
		t := fleetJoinTarget{ID: id, Name: fmt.Sprintf("server #%d", id)}
		if m.ctx.DB != nil {
			var srv storage.Server
			if m.ctx.DB.First(&srv, id).Error == nil {
				t.Name = srv.Name
				t.Host = srv.Host
			}
		}
		out = append(out, t)
	}
	return out
}

func (m *Model) joinSelectedFleet() tea.Cmd {
	if m.joinCursor < 0 || m.joinCursor >= len(m.joinTargets) {
		return nil
	}
	t := m.joinTargets[m.joinCursor]
	m.swarmUI = swarmModeList
	m.busy = true
	m.status = "Joining " + t.Name + "…"
	managerID := m.ctx.ServerID
	workerID := t.ID
	return func() tea.Msg {
		mgrEx, ok := m.ctx.Pool.GetExecutor(managerID)
		if !ok {
			return actionDoneMsg{err: "Not connected."}
		}
		workerEx, ok := m.ctx.Pool.GetExecutor(workerID)
		if !ok {
			return actionDoneMsg{err: t.Name + " is not connected."}
		}
		mgr := xmdocker.NewManager(mgrEx)
		token, err := mgr.JoinToken(xmdocker.RoleWorker)
		if err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		info := mgr.SwarmInfo()
		addr := xmdocker.JoinListenAddr(info.NodeAddr, info.AdvertiseAddr)
		if addr == "" {
			cmd, jerr := mgr.JoinCommand(xmdocker.RoleWorker)
			if jerr != nil {
				return actionDoneMsg{err: "no advertise address"}
			}
			var parsed bool
			_, addr, parsed = xmdocker.ParseJoinParts(cmd)
			if !parsed {
				return actionDoneMsg{err: "could not parse join address"}
			}
		}
		if err := xmdocker.JoinRemote(workerEx, token, addr); err != nil {
			return actionDoneMsg{err: err.Error()}
		}
		return actionDoneMsg{brief: t.Name + " joined as worker"}
	}
}

func (m *Model) selectedSwarmNode() (xmdocker.SwarmNode, bool) {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.swarmNodes) {
		return xmdocker.SwarmNode{}, false
	}
	return m.swarmNodes[idx], true
}

func (m *Model) swarmPanel(inner int) string {
	if m.tab != tabSwarm {
		return ""
	}
	var b strings.Builder
	state := m.swarm.State
	if state == "" {
		state = "inactive"
	}
	b.WriteString(theme.MutedText().Render(components.Wrap(
		fmt.Sprintf("Swarm %s", state), inner)))
	b.WriteByte('\n')
	if !m.swarm.Active() {
		b.WriteString(theme.MutedText().Render(components.Wrap(
			"Off until you init on this Docker host. New nodes need Docker Engine only — paste the join command there. Press i to init.", inner)))
		return b.String()
	}
	if !m.swarm.Manager() {
		b.WriteString(theme.MutedText().Render(components.Wrap(
			"This node is in the swarm as a worker. L leaves. Join commands are shown on a manager.", inner)))
		return b.String()
	}
	if m.workerJoin != "" {
		b.WriteString(theme.MutedText().Render("Worker join (paste on a host with Docker) · c copy"))
		b.WriteByte('\n')
		b.WriteString(theme.KeyStyle().Render(components.Wrap(m.workerJoin, inner)))
	}
	if m.managerJoin != "" {
		b.WriteByte('\n')
		b.WriteString(theme.MutedText().Render("Manager join"))
		b.WriteByte('\n')
		b.WriteString(theme.MutedText().Render(components.Wrap(m.managerJoin, inner)))
	}
	if len(m.swarmSvcs) > 0 {
		b.WriteByte('\n')
		b.WriteString(theme.MutedText().Render("Services (read-only)"))
		for _, s := range m.swarmSvcs {
			b.WriteByte('\n')
			b.WriteString(theme.MutedText().Render(components.Wrap(
				fmt.Sprintf("  %s  %s  %s  %s", s.Name, s.Mode, s.Replicas, s.Image), inner)))
		}
	}
	return b.String()
}

func (m *Model) swarmOverlay() string {
	switch m.swarmUI {
	case swarmModeConfirmLeave:
		return theme.WarningText().Render("Leave swarm on this node? (y/n)")
	case swarmModeConfirmRemove:
		return theme.WarningText().Render("Remove selected node from the swarm? (y/n)")
	case swarmModeJoinPick:
		var b strings.Builder
		b.WriteString(theme.MutedText().Render("Run the worker join command over SSH:"))
		b.WriteByte('\n')
		for i, t := range m.joinTargets {
			line := t.Name
			if t.Host != "" {
				line += "  " + t.Host
			}
			if i == m.joinCursor {
				b.WriteString(theme.KeyStyle().Render("> " + line))
			} else {
				b.WriteString(theme.MutedText().Render("  " + line))
			}
			b.WriteByte('\n')
		}
		b.WriteString(theme.MutedText().Render("Enter join · esc cancel"))
		return b.String()
	default:
		return ""
	}
}

func (m *Model) swarmChromeRows(inner int) int {
	n := 0
	if p := m.swarmPanel(inner); p != "" {
		n += strings.Count(p, "\n") + 1
	}
	if o := m.swarmOverlay(); o != "" {
		n += strings.Count(o, "\n") + 2
	}
	return n
}

func (m *Model) handleSwarmKey(msg tea.KeyMsg) (shared.Screen, tea.Cmd, bool) {
	if m.tab != tabSwarm {
		return m, nil, false
	}
	key := msg.String()
	switch m.swarmUI {
	case swarmModeConfirmLeave:
		if key == "y" || key == "Y" {
			return m, m.swarmLeave(), true
		}
		if key == "n" || key == "N" || key == "esc" {
			m.swarmUI = swarmModeList
			return m, nil, true
		}
		return m, nil, true
	case swarmModeConfirmRemove:
		if key == "y" || key == "Y" {
			return m, m.swarmRemove(), true
		}
		if key == "n" || key == "N" || key == "esc" {
			m.swarmUI = swarmModeList
			m.pendingNodeID = ""
			return m, nil, true
		}
		return m, nil, true
	case swarmModeJoinPick:
		switch key {
		case "esc":
			m.swarmUI = swarmModeList
			return m, nil, true
		case "up", "k":
			if m.joinCursor > 0 {
				m.joinCursor--
			}
			return m, nil, true
		case "down", "j":
			if m.joinCursor < len(m.joinTargets)-1 {
				m.joinCursor++
			}
			return m, nil, true
		case "enter":
			return m, m.joinSelectedFleet(), true
		}
		return m, nil, true
	}

	switch key {
	case "i":
		return m, m.swarmInit(), true
	case "c":
		return m, m.copyWorkerJoin(), true
	case "j":
		return m, m.openJoinPicker(), true
	case "p":
		return m, m.swarmPromote(), true
	case "m":
		return m, m.swarmDemote(), true
	case "a":
		return m, m.swarmCycleAvailability(), true
	case "x":
		n, ok := m.selectedSwarmNode()
		if !ok {
			return m, nil, true
		}
		m.pendingNodeID = n.ID
		m.swarmUI = swarmModeConfirmRemove
		return m, nil, true
	case "L":
		m.swarmUI = swarmModeConfirmLeave
		return m, nil, true
	}
	return m, nil, false
}
