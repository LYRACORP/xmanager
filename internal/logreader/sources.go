package logreader

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// Source IDs for the web Logs page.
const (
	SourcePanel     = "panel"
	SourceSystem    = "system"
	SourceSSH       = "ssh"
	SourceWebserver = "webserver"
	SourceProjects  = "projects"
	SourceDocker    = "docker"
	SourceActivity  = "activity"
)

const (
	DefaultLines = 300
	MaxBytes     = 256 * 1024
)

// Options for Tail.
type Options struct {
	Lines     int
	Project   string // slug or name for projects source
	Container string // container id/name for docker source
	Projects  []ProjectRef
}

// ProjectRef is a project to sample when no specific project is selected.
type ProjectRef struct {
	Slug string
	Name string
}

// Result is the output of a Tail call.
type Result struct {
	Source string
	Label  string
	Text   string
}

// IsValidSource reports whether id is a known non-activity source.
func IsValidSource(id string) bool {
	switch id {
	case SourcePanel, SourceSystem, SourceSSH, SourceWebserver, SourceProjects, SourceDocker, SourceActivity:
		return true
	default:
		return false
	}
}

// Sources returns tab definitions (excluding activity which is DB-backed).
func Sources() []struct{ ID, Label string } {
	return []struct{ ID, Label string }{
		{SourcePanel, "Panel"},
		{SourceSystem, "System"},
		{SourceSSH, "SSH"},
		{SourceWebserver, "Webserver"},
		{SourceProjects, "Projects"},
		{SourceDocker, "Docker"},
		{SourceActivity, "Activity"},
	}
}

// Tail runs an allowlisted log command for the given source.
func Tail(exec *ssh.Executor, source string, opts Options) (Result, error) {
	if exec == nil {
		return Result{}, fmt.Errorf("no executor")
	}
	lines := opts.Lines
	if lines <= 0 {
		lines = DefaultLines
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = SourcePanel
	}

	var cmds []namedCmd
	var label string
	switch source {
	case SourcePanel:
		label = "Panel (xmanager-web)"
		cmds = panelCommands(lines)
	case SourceSystem:
		label = "System journal / syslog"
		cmds = systemCommands(lines)
	case SourceSSH:
		label = "SSH / auth"
		cmds = sshCommands(lines)
	case SourceWebserver:
		label = "Webserver"
		cmds = webserverCommands(lines)
	case SourceProjects:
		label = "Projects"
		return tailProjects(exec, opts, lines)
	case SourceDocker:
		label = "Docker"
		return tailDocker(exec, opts, lines)
	default:
		return Result{}, fmt.Errorf("unknown source %q", source)
	}

	text, used := runFirst(exec, cmds)
	if used != "" {
		label = label + " · " + used
	}
	return Result{Source: source, Label: label, Text: truncate(text)}, nil
}

type namedCmd struct {
	Name string
	Cmd  string
}

func panelCommands(n int) []namedCmd {
	return []namedCmd{
		{"journalctl", fmt.Sprintf("journalctl -u xmanager-web -u xmanager -n %d --no-pager 2>/dev/null", n)},
		{"xmanager logs", fmt.Sprintf("sh -c 'ls /var/log/xmanager*.log 2>/dev/null | head -5 | xargs -r tail -n %d 2>/dev/null'", n)},
	}
}

func systemCommands(n int) []namedCmd {
	return []namedCmd{
		{"journalctl", fmt.Sprintf("journalctl -n %d --no-pager 2>/dev/null", n)},
		{"syslog", fmt.Sprintf("tail -n %d /var/log/syslog 2>/dev/null", n)},
		{"messages", fmt.Sprintf("tail -n %d /var/log/messages 2>/dev/null", n)},
	}
}

func sshCommands(n int) []namedCmd {
	return []namedCmd{
		{"journalctl ssh", fmt.Sprintf("journalctl -u ssh -u sshd -n %d --no-pager 2>/dev/null", n)},
		{"auth.log", fmt.Sprintf("tail -n %d /var/log/auth.log 2>/dev/null", n)},
		{"secure", fmt.Sprintf("tail -n %d /var/log/secure 2>/dev/null", n)},
	}
}

func webserverCommands(n int) []namedCmd {
	half := n / 2
	if half < 50 {
		half = 100
	}
	return []namedCmd{
		{"nginx", fmt.Sprintf(
			`sh -c 'echo "=== nginx access ==="; tail -n %d /var/log/nginx/access.log 2>/dev/null; echo; echo "=== nginx error ==="; tail -n %d /var/log/nginx/error.log 2>/dev/null'`,
			half, half)},
		{"caddy", fmt.Sprintf("journalctl -u caddy -n %d --no-pager 2>/dev/null", n)},
		{"nginx journal", fmt.Sprintf("journalctl -u nginx -n %d --no-pager 2>/dev/null", n)},
	}
}

func tailProjects(exec *ssh.Executor, opts Options, lines int) (Result, error) {
	slug := sanitizeID(opts.Project)
	if slug != "" {
		text := exec.RunQuiet(projectLogCmd(slug, lines))
		if strings.TrimSpace(text) == "" {
			text = "(no logs for project " + slug + ")"
		}
		return Result{
			Source: SourceProjects,
			Label:  "Project · " + slug,
			Text:   truncate(text),
		}, nil
	}
	if len(opts.Projects) == 0 {
		return Result{
			Source: SourceProjects,
			Label:  "Projects",
			Text:   "No projects yet. Create a project or pick one from the dropdown.",
		}, nil
	}
	var b strings.Builder
	limit := 8
	per := 80
	if lines > 0 && lines < per {
		per = lines
	}
	for i, p := range opts.Projects {
		if i >= limit {
			b.WriteString("\n… (more projects omitted — select one above)\n")
			break
		}
		s := sanitizeID(p.Slug)
		if s == "" {
			s = sanitizeID(p.Name)
		}
		if s == "" {
			continue
		}
		b.WriteString("=== ")
		b.WriteString(p.Name)
		if p.Name != s {
			b.WriteString(" (")
			b.WriteString(s)
			b.WriteString(")")
		}
		b.WriteString(" ===\n")
		out := exec.RunQuiet(projectLogCmd(s, per))
		if strings.TrimSpace(out) == "" {
			b.WriteString("(empty)\n")
		} else {
			b.WriteString(out)
			if !strings.HasSuffix(out, "\n") {
				b.WriteByte('\n')
			}
		}
		b.WriteByte('\n')
	}
	return Result{Source: SourceProjects, Label: "Projects (overview)", Text: truncate(b.String())}, nil
}

func projectLogCmd(slug string, lines int) string {
	return fmt.Sprintf(
		`cd /opt/xmanager/projects/%s 2>/dev/null && docker compose logs --tail=%d 2>/dev/null || docker logs --tail=%d %s 2>/dev/null || echo "(no compose/container logs)"`,
		slug, lines, lines, slug,
	)
}

func tailDocker(exec *ssh.Executor, opts Options, lines int) (Result, error) {
	id := sanitizeID(opts.Container)
	if id == "" {
		list := exec.RunQuiet(`docker ps --format '{{.ID}}\t{{.Names}}\t{{.Status}}' 2>/dev/null`)
		if strings.TrimSpace(list) == "" {
			list = "No running containers. Start a container or pick an ID."
		} else {
			list = "Select a container above, or open Docker.\n\nID\tNAME\tSTATUS\n" + list
		}
		return Result{Source: SourceDocker, Label: "Docker containers", Text: truncate(list)}, nil
	}
	text := exec.RunQuiet(fmt.Sprintf("docker logs --tail %d %s 2>&1", lines, shellQuote(id)))
	if strings.TrimSpace(text) == "" {
		text = "(no docker logs for " + id + ")"
	}
	return Result{Source: SourceDocker, Label: "Docker · " + id, Text: truncate(text)}, nil
}

func runFirst(exec *ssh.Executor, cmds []namedCmd) (text, used string) {
	for _, c := range cmds {
		out := exec.RunQuiet(c.Cmd)
		out = strings.TrimSpace(out)
		if out == "" || isEmptyJournal(out) {
			continue
		}
		return out, c.Name
	}
	return "(no log output found for this source)", ""
}

func isEmptyJournal(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "-- no entries --") ||
		strings.Contains(low, "no entries") && len(s) < 80
}

func truncate(s string) string {
	if len(s) <= MaxBytes {
		return s
	}
	return s[len(s)-MaxBytes:] + "\n… (truncated)"
}

var safeIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$`)

// sanitizeID allows only safe docker/project identifiers.
func sanitizeID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\n;$`|&;<>") {
		return ""
	}
	// Project slugs are usually alphanumeric with dashes
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-', r == '_', r == '.', r == '/':
			b.WriteRune(r)
		default:
			return ""
		}
	}
	out := b.String()
	if !safeIDRe.MatchString(out) && out != "" {
		// still allow short hex docker ids
		if len(out) < 3 {
			return ""
		}
	}
	return out
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

// PanelCommandsForTest exports panel command list for tests.
func PanelCommandsForTest(n int) []string {
	var out []string
	for _, c := range panelCommands(n) {
		out = append(out, c.Cmd)
	}
	return out
}

// SystemCommandsForTest exports system commands for tests.
func SystemCommandsForTest(n int) []string {
	var out []string
	for _, c := range systemCommands(n) {
		out = append(out, c.Cmd)
	}
	return out
}

// SanitizeIDForTest exports sanitizeID for tests.
func SanitizeIDForTest(s string) string { return sanitizeID(s) }

// TruncateForTest exports truncate for tests.
func TruncateForTest(s string) string { return truncate(s) }
