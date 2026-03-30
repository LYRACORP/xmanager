package backup

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/shared"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

type screenMode int

const (
	modeList screenMode = iota
	modeFormCreate
	modeConfirmDelete
	modeConfirmRestore
	modeFormSchedule
)

var backupTypes = []string{"postgres", "mysql", "mongodb", "volume"}

type backupsLoadedMsg struct {
	backups []storage.Backup
	err     error
}

type infoToastMsg struct {
	text string
}

type Model struct {
	ctx     *shared.AppContext
	mode    screenMode
	width   int
	height  int
	message string

	backups []storage.Backup
	table   table.Model

	typeIdx int
	svcIn   textinput.Model
	pathIn  textinput.Model
	schedIn textinput.Model
	formIdx int

	scheduleEdit textinput.Model
	editBackupID uint
}

func New(ctx *shared.AppContext) *Model {
	m := &Model{ctx: ctx}
	m.svcIn = textinput.New()
	m.svcIn.Placeholder = "db name or container"
	m.svcIn.Prompt = "Service: "
	m.svcIn.Width = 40

	m.pathIn = textinput.New()
	m.pathIn.Placeholder = "/var/lib/postgresql/data or connection ref"
	m.pathIn.Prompt = "Path: "
	m.pathIn.Width = 40

	m.schedIn = textinput.New()
	m.schedIn.Placeholder = "0 2 * * *"
	m.schedIn.Prompt = "Schedule: "
	m.schedIn.Width = 40

	m.scheduleEdit = textinput.New()
	m.scheduleEdit.Placeholder = "cron expression"
	m.scheduleEdit.Prompt = "Schedule: "
	m.scheduleEdit.Width = 40

	return m
}

func (m *Model) Name() string     { return "Backup" }
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h; m.rebuildTable() }

func (m *Model) Init() tea.Cmd {
	return m.loadBackups
}

func (m *Model) loadBackups() tea.Msg {
	if m.ctx.DB == nil {
		return backupsLoadedMsg{err: fmt.Errorf("database not available")}
	}
	var list []storage.Backup
	q := m.ctx.DB.Order("backed_at desc").Limit(500)
	if m.ctx.ServerID > 0 {
		q = q.Where("server_id = ?", m.ctx.ServerID)
	}
	if err := q.Find(&list).Error; err != nil {
		return backupsLoadedMsg{err: err}
	}
	return backupsLoadedMsg{backups: list}
}

func formatSize(n int64) string {
	if n <= 0 {
		return "—"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", f, units[i])
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func ageShort(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func (m *Model) rebuildTable() {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	cols := []table.Column{
		{Title: "Type", Width: 10},
		{Title: "Service", Width: 14},
		{Title: "Path", Width: 22},
		{Title: "Size", Width: 10},
		{Title: "Age", Width: 12},
		{Title: "Schedule", Width: 14},
		{Title: "Status", Width: 12},
	}
	rows := make([]table.Row, len(m.backups))
	for i, b := range m.backups {
		rows[i] = table.Row{
			b.Type,
			b.Service,
			trimW(b.Path, 22),
			formatSize(b.Size),
			ageShort(b.BackedAt),
			trimW(b.Schedule, 14),
			b.Status,
		}
	}
	m.table = components.StyledTable(cols, rows, h)
}

func trimW(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w < 4 {
		return s[:w]
	}
	return s[:w-1] + "…"
}

func (m *Model) Update(msg tea.Msg) (shared.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case backupsLoadedMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.backups = msg.backups
			m.message = ""
			if m.mode == modeFormCreate || m.mode == modeFormSchedule {
				m.mode = modeList
			}
		}
		m.rebuildTable()
		return m, nil

	case infoToastMsg:
		m.message = msg.text
		m.mode = modeList
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeFormCreate:
			return m.updateFormCreate(msg)
		case modeConfirmDelete, modeConfirmRestore:
			switch msg.String() {
			case "y", "Y":
				if m.mode == modeConfirmDelete {
					m.mode = modeList
					return m, m.doDelete()
				}
				m.mode = modeList
				return m, m.doRestore()
			case "n", "N", "esc":
				m.mode = modeList
				return m, nil
			}
			return m, nil
		case modeFormSchedule:
			switch msg.String() {
			case "esc":
				m.mode = modeList
				return m, nil
			case "enter":
				return m, m.saveSchedule()
			}
			var cmd tea.Cmd
			m.scheduleEdit, cmd = m.scheduleEdit.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return shared.GoBackMsg{} }
		case "r":
			return m, m.loadBackups
		case "c":
			m.mode = modeFormCreate
			m.typeIdx = 0
			m.formIdx = 0
			m.svcIn.SetValue("")
			m.pathIn.SetValue("")
			m.schedIn.SetValue("")
			m.svcIn.Blur()
			m.pathIn.Blur()
			m.schedIn.Blur()
			m.svcIn.Focus()
			return m, textinput.Blink
		case "d":
			if idx := m.table.Cursor(); idx >= 0 && idx < len(m.backups) {
				m.mode = modeConfirmDelete
				m.editBackupID = m.backups[idx].ID
			}
			return m, nil
		case "R":
			if idx := m.table.Cursor(); idx >= 0 && idx < len(m.backups) {
				m.mode = modeConfirmRestore
				m.editBackupID = m.backups[idx].ID
			}
			return m, nil
		case "s":
			if idx := m.table.Cursor(); idx >= 0 && idx < len(m.backups) {
				b := m.backups[idx]
				m.mode = modeFormSchedule
				m.editBackupID = b.ID
				m.scheduleEdit.SetValue(b.Schedule)
				m.scheduleEdit.Focus()
			}
			return m, textinput.Blink
		}
	}

	if m.mode == modeList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

const formFieldCount = 3

func (m *Model) updateFormCreate(msg tea.KeyMsg) (shared.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "t":
		m.typeIdx = (m.typeIdx + 1) % len(backupTypes)
		return m, nil
	case "tab", "down":
		m.blurAllForm()
		m.formIdx = (m.formIdx + 1) % formFieldCount
		m.focusFormField()
		return m, textinput.Blink
	case "shift+tab", "up":
		m.blurAllForm()
		m.formIdx = (m.formIdx - 1 + formFieldCount) % formFieldCount
		m.focusFormField()
		return m, textinput.Blink
	case "enter":
		if m.formIdx < formFieldCount-1 {
			m.blurAllForm()
			m.formIdx++
			m.focusFormField()
			return m, textinput.Blink
		}
		return m, m.saveNewBackup()
	}
	var cmd tea.Cmd
	switch m.formIdx {
	case 0:
		m.svcIn, cmd = m.svcIn.Update(msg)
	case 1:
		m.pathIn, cmd = m.pathIn.Update(msg)
	default:
		m.schedIn, cmd = m.schedIn.Update(msg)
	}
	return m, cmd
}

func (m *Model) blurAllForm() {
	m.svcIn.Blur()
	m.pathIn.Blur()
	m.schedIn.Blur()
}

func (m *Model) focusFormField() {
	switch m.formIdx {
	case 0:
		m.svcIn.Focus()
	case 1:
		m.pathIn.Focus()
	default:
		m.schedIn.Focus()
	}
}

func (m *Model) saveNewBackup() tea.Cmd {
	t := backupTypes[m.typeIdx]
	svc := strings.TrimSpace(m.svcIn.Value())
	path := strings.TrimSpace(m.pathIn.Value())
	sched := strings.TrimSpace(m.schedIn.Value())
	if m.ctx.ServerID == 0 {
		return func() tea.Msg {
			return backupsLoadedMsg{err: fmt.Errorf("select a server before creating backups")}
		}
	}
	if svc == "" || path == "" {
		return func() tea.Msg {
			return backupsLoadedMsg{err: fmt.Errorf("service and path are required")}
		}
	}
	return func() tea.Msg {
		b := storage.Backup{
			ServerID: m.ctx.ServerID,
			Type:     t,
			Service:  svc,
			Path:     path,
			Schedule: sched,
			Status:   "success",
			BackedAt: time.Now(),
		}
		if err := m.ctx.DB.Create(&b).Error; err != nil {
			return backupsLoadedMsg{err: err}
		}
		return m.loadBackups()
	}
}

func (m *Model) saveSchedule() tea.Cmd {
	id := m.editBackupID
	v := strings.TrimSpace(m.scheduleEdit.Value())
	return func() tea.Msg {
		if err := m.ctx.DB.Model(&storage.Backup{}).Where("id = ?", id).Update("schedule", v).Error; err != nil {
			return backupsLoadedMsg{err: err}
		}
		return m.loadBackups()
	}
}

func (m *Model) doDelete() tea.Cmd {
	id := m.editBackupID
	return func() tea.Msg {
		if err := m.ctx.DB.Delete(&storage.Backup{}, id).Error; err != nil {
			return backupsLoadedMsg{err: err}
		}
		return m.loadBackups()
	}
}

func (m *Model) doRestore() tea.Cmd {
	idx := -1
	for i := range m.backups {
		if m.backups[i].ID == m.editBackupID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return func() tea.Msg { return infoToastMsg{text: "backup not found"} }
	}
	b := m.backups[idx]
	line := fmt.Sprintf("Restore requested for %s / %s (type=%s) — run pg_restore/mongorestore/etc. on the host.", b.Service, b.Path, b.Type)
	return func() tea.Msg { return infoToastMsg{text: line} }
}

func (m *Model) View() string {
	switch m.mode {
	case modeFormCreate:
		return m.viewFormCreate()
	case modeFormSchedule:
		header := theme.HeaderStyle().Render("Schedule")
		foot := theme.MutedText().Render("  Enter: save  Esc: cancel")
		return lipgloss.JoinVertical(lipgloss.Left, header, "", m.scheduleEdit.View(), "", foot)
	case modeConfirmDelete:
		return m.viewList() + "\n\n " + theme.WarningText().Render("Delete this backup record? (y/n)")
	case modeConfirmRestore:
		return m.viewList() + "\n\n " + theme.WarningText().Render("Confirm restore workflow for this backup? (y/n)")
	default:
		return m.viewList()
	}
}

func (m *Model) viewFormCreate() string {
	header := theme.HeaderStyle().Render("Create backup")
	typeLine := fmt.Sprintf("  Type: %s  (%s)", backupTypes[m.typeIdx], theme.MutedText().Render("t: cycle"))
	cursorSvc, cursorPath, cursorSched := "  ", "  ", "  "
	switch m.formIdx {
	case 0:
		cursorSvc = theme.KeyStyle().Render("> ")
	case 1:
		cursorPath = theme.KeyStyle().Render("> ")
	default:
		cursorSched = theme.KeyStyle().Render("> ")
	}
	form := typeLine + "\n" + cursorSvc + m.svcIn.View() + "\n" + cursorPath + m.pathIn.View() + "\n" + cursorSched + m.schedIn.View()
	foot := theme.MutedText().Render("  Tab: field  t: type  Enter: save  Esc: cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", form, "", foot)
}

func (m *Model) viewList() string {
	title := theme.HeaderStyle().Render("Backups")
	filter := ""
	if m.ctx.ServerID > 0 {
		filter = theme.MutedText().Render(fmt.Sprintf(" (server %d)", m.ctx.ServerID))
	}
	help := components.NewHelpBar(
		components.KeyBinding{Key: "c", Desc: "create"},
		components.KeyBinding{Key: "R", Desc: "restore"},
		components.KeyBinding{Key: "d", Desc: "delete"},
		components.KeyBinding{Key: "s", Desc: "schedule"},
		components.KeyBinding{Key: "r", Desc: "refresh"},
		components.KeyBinding{Key: "esc", Desc: "back"},
	)
	help.Width = m.width
	msg := ""
	if m.message != "" {
		msg = "\n " + m.message
	}
	return lipgloss.JoinVertical(lipgloss.Left, title+filter, m.table.View(), msg, help.View())
}
