package dashboard

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/tui/components"
	"github.com/lyracorp/xmanager/internal/tui/layout"
	"github.com/lyracorp/xmanager/internal/tui/theme"
)

const maxPreviewBytes = 512 * 1024

type dirEntry struct {
	name     string
	fullPath string
	isDir    bool
	isParent bool
	size     int64
	modTime  time.Time
	mode     string
}

type dirLoadedMsg struct {
	path    string
	entries []dirEntry
	err     error
}

type filePreviewMsg struct {
	path string
	body string
	err  error
}

func (m *Model) loadDir() tea.Cmd {
	m.dirState = stateLoading
	serverID := m.ctx.ServerID
	pool := m.ctx.Pool
	curPath := m.curPath
	return func() tea.Msg {
		if pool == nil {
			return dirLoadedMsg{path: curPath, err: errNotConnected}
		}
		var entries []dirEntry
		err := pool.WithSFTP(serverID, func(sftp *ssh.SFTPClient) error {
			infos, listErr := sftp.ListDir(curPath)
			if listErr != nil {
				return listErr
			}
			for _, info := range infos {
				name := info.Name()
				if name == "." {
					continue
				}
				full := path.Join(curPath, name)
				if curPath == "/" && !strings.HasPrefix(full, "/") {
					full = "/" + name
				}
				entries = append(entries, dirEntry{
					name:     name,
					fullPath: full,
					isDir:    info.IsDir(),
					size:     info.Size(),
					modTime:  info.ModTime(),
					mode:     info.Mode().String(),
				})
			}
			return nil
		})
		if err != nil {
			return dirLoadedMsg{path: curPath, err: err}
		}

		if curPath != "/" {
			parent := parentPath(curPath)
			entries = append([]dirEntry{{
				name:     "..",
				fullPath: parent,
				isDir:    true,
				isParent: true,
			}}, entries...)
		}

		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].isParent {
				return true
			}
			if entries[j].isParent {
				return false
			}
			if entries[i].isDir != entries[j].isDir {
				return entries[i].isDir
			}
			return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
		})

		return dirLoadedMsg{path: curPath, entries: entries}
	}
}

func (m *Model) loadFilePreview(filePath string, size int64) tea.Cmd {
	if size > maxPreviewBytes {
		return func() tea.Msg {
			return filePreviewMsg{
				path: filePath,
				body: fmt.Sprintf("File too large to preview (%s)", components.HumanBytes(size)),
			}
		}
	}
	serverID := m.ctx.ServerID
	pool := m.ctx.Pool
	return func() tea.Msg {
		var data []byte
		err := pool.WithSFTP(serverID, func(sftp *ssh.SFTPClient) error {
			var readErr error
			data, readErr = sftp.ReadFile(filePath)
			return readErr
		})
		if err != nil {
			return filePreviewMsg{path: filePath, err: err}
		}
		body := formatPreviewBody(data)
		return filePreviewMsg{path: filePath, body: body}
	}
}

func formatPreviewBody(data []byte) string {
	if len(data) == 0 {
		return "(empty file)"
	}
	if !utf8.Valid(data) {
		return fmt.Sprintf("binary file, %s", components.HumanBytes(int64(len(data))))
	}
	s := string(data)
	lines := strings.Split(s, "\n")
	if len(lines) > 500 {
		s = strings.Join(lines[:500], "\n") + "\n…"
	}
	return s
}

func parentPath(p string) string {
	if p == "/" || p == "" {
		return "/"
	}
	p = path.Clean(p)
	parent := path.Dir(p)
	if parent == "." {
		return "/"
	}
	return parent
}

func (m *Model) rebuildDirTable() {
	cols := layout.AdaptiveColumns(m.width, []table.Column{
		{Title: " ", Width: 3},
		{Title: "Name", Width: 24},
		{Title: "Type", Width: 6},
		{Title: "Size", Width: 10},
		{Title: "Modified", Width: 16},
		{Title: "Mode", Width: 0},
	})
	rows := make([]table.Row, len(m.dirEntries))
	for i, e := range m.dirEntries {
		icon := "file"
		if e.isDir {
			icon = "dir"
		}
		if e.isParent {
			icon = "up"
		}
		sizeStr := ""
		if !e.isDir && !e.isParent {
			sizeStr = components.HumanBytes(e.size)
		}
		mod := ""
		if !e.modTime.IsZero() {
			mod = e.modTime.Format("2006-01-02 15:04")
		}
		rows[i] = table.Row{
			dirIcon(icon),
			e.name,
			icon,
			sizeStr,
			mod,
			e.mode,
		}
	}
	h := m.dirTableHeight()
	m.dirTable = m.dirTable.SetData(m.width, cols, rows, h)
}

func (m *Model) dirLocalChrome() int {
	chrome := components.FrameChromeRows(true) + components.TabBarRows() + 2
	if m.previewOpen && !m.previewOverlay {
		chrome += 2
	}
	return chrome
}

func (m *Model) dirTableHeight() int {
	return layout.BodyHeight(m.height, m.dirLocalChrome(), 5)
}

func dirIcon(kind string) string {
	switch kind {
	case "dir":
		return theme.MutedText().Render("/")
	case "up":
		return theme.MutedText().Render("^")
	default:
		return " "
	}
}

func (m *Model) renderFiles() string {
	breadcrumb := renderBreadcrumb(m.curPath, m.width)
	if m.pathInputMode {
		input := components.ApplyInputTheme(m.pathInput, m.width, true)
		return breadcrumb + "\n" + theme.SubtitleStyle().Render("Go to path") + "\n" + input.View() + "\n" +
			theme.MutedText().Render("  Enter confirm · Esc cancel")
	}

	if m.dirState == stateLoading && len(m.dirEntries) == 0 {
		return breadcrumb + "\n" + theme.MutedText().Render("  Loading directory…")
	}
	if m.dirState == stateError {
		return breadcrumb + "\n" + theme.ErrorText().Render("  "+m.dirErr)
	}

	listContent := theme.EmptyStateText()
	if len(m.dirEntries) > 0 {
		listContent = m.dirTable.View()
	}

	if m.previewOpen && !m.previewOverlay {
		leftW, rightW, stack := splitPanels(m.width, 1, 50, 28, 24)
		listPanel := theme.PanelStyle().Width(leftW).Render(listContent)
		previewTitle := theme.SubtitleStyle().Render(m.previewPath)
		previewBody := theme.ViewportStyle().Width(rightW - 2).Render(m.previewScroll.View())
		previewPanel := theme.PanelStyle().Width(rightW).Render(previewTitle + "\n" + previewBody)
		if stack {
			return breadcrumb + "\n" + lipgloss.JoinVertical(lipgloss.Left, listPanel, previewPanel)
		}
		return breadcrumb + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, listPanel, " ", previewPanel)
	}

	body := theme.PanelStyle().Width(layout.PanelWidth(m.width)).Render(listContent)
	out := breadcrumb + "\n" + body

	if m.previewOpen && m.previewOverlay {
		overlay := theme.ActivePanelStyle().Width(m.width - 2).Render(
			theme.SubtitleStyle().Render(m.previewPath) + "\n" +
				theme.ViewportStyle().Width(m.width - 6).Render(m.previewScroll.View()) + "\n" +
				theme.MutedText().Render("  Esc close preview"),
		)
		out += "\n" + overlay
	}
	return out
}

func renderBreadcrumb(curPath string, width int) string {
	label := theme.SubtitleStyle().Render("Path: ") + theme.MutedText().Render(truncateBreadcrumb(curPath, width-10))
	return label
}

func truncateBreadcrumb(p string, max int) string {
	if len(p) <= max {
		return p
	}
	if max <= 8 {
		return "…" + p[len(p)-max+1:]
	}
	keep := (max - 3) / 2
	return p[:keep] + "…" + p[len(p)-keep:]
}
