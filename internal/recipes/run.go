package recipes

import (
	"fmt"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// RunResult is the outcome of executing a recipe.
type RunResult struct {
	OK     bool
	Output string
	Creds  string
	Step   string
	Err    error
}

// Runner executes recipe commands over SSH.
type Runner struct {
	Exec     *ssh.Executor
	User     string // remote SSH user; non-root commands are elevated with sudo
	Password string // sudo password (same as SSH password when password sudo)
}

func (r Runner) needsSudo() bool {
	u := strings.TrimSpace(r.User)
	return u != "" && u != "root"
}

// remoteShell wraps cmd for remote bash, elevating with sudo when needed.
func (r Runner) remoteShell(cmd string) string {
	quoted := shellQuote(cmd)
	if !r.needsSudo() {
		return "bash -lc " + quoted
	}
	if pass := strings.TrimSpace(r.Password); pass != "" {
		return fmt.Sprintf("printf '%%s\\n' %s | sudo -S -p '' -E bash -lc %s", shellQuote(pass), quoted)
	}
	return "sudo -n -E bash -lc " + quoted
}

const (
	aptLockWaitSecs   = 5
	aptLockMaxWaits   = 120 // ~10 minutes
	aptLockMaxRetries = 24  // extra retries if lock races after wait
)

// Run executes all steps, calling onProgress between commands.
func (r Runner) Run(recipe Recipe, onProgress ProgressFunc) RunResult {
	if r.Exec == nil {
		return RunResult{Err: fmt.Errorf("no SSH executor")}
	}
	mat, creds, err := Materialize(recipe)
	if err != nil {
		return RunResult{Err: err}
	}

	total := mat.CommandCount()
	if total == 0 {
		return RunResult{OK: true, Creds: creds, Output: "nothing to run"}
	}

	var log strings.Builder
	if creds != "" {
		log.WriteString("Generated credentials (save these):\n")
		log.WriteString(creds)
		log.WriteString("\n\n")
	}

	done := 0
	report := func(detail string) {
		if onProgress == nil {
			return
		}
		pct := float64(done) / float64(total)
		if pct > 1 {
			pct = 1
		}
		onProgress(pct, detail)
	}

	report(fmt.Sprintf("Starting %s…", mat.Name))
	if r.needsSudo() {
		probe := r.remoteShell("true")
		if res, err := r.Exec.Run(probe); err != nil {
			return RunResult{Err: fmt.Errorf("sudo check failed: %w", err)}
		} else if res != nil && res.ExitCode != 0 {
			msg := strings.TrimSpace(res.Stdout + " " + res.Stderr)
			if strings.TrimSpace(r.Password) == "" {
				return RunResult{Err: fmt.Errorf("user %q needs sudo — store the Ubuntu password on the server entry in the TUI, or configure NOPASSWD sudo", r.User)}
			}
			return RunResult{Err: fmt.Errorf("sudo failed for user %q (check Password on server entry): %s", r.User, msg)}
		}
	}
	for _, step := range mat.Steps {
		report(step.Name)
		log.WriteString("==> " + step.Name + "\n")
		for _, cmd := range step.Commands {
			detail := truncate(strings.ReplaceAll(cmd, "\n", " "), 72)
			report(step.Name + ": " + detail)

			res, err := r.runCommand(cmd, usesApt(cmd), func(msg string) {
				report(msg)
				log.WriteString(msg + "\n")
			})
			done++
			if err != nil {
				msg := fmt.Sprintf("SSH error: %v", err)
				log.WriteString(msg + "\n")
				report(msg)
				return RunResult{OK: false, Output: log.String(), Creds: creds, Step: step.Name, Err: err}
			}
			chunk := strings.TrimSpace(res.Stdout)
			if res.Stderr != "" {
				if chunk != "" {
					chunk += "\n"
				}
				chunk += strings.TrimSpace(res.Stderr)
			}
			if chunk != "" {
				log.WriteString(chunk + "\n")
			}
			if res.ExitCode != 0 {
				msg := fmt.Sprintf("exit %d", res.ExitCode)
				log.WriteString(msg + "\n")
				report(step.Name + " failed (" + msg + ")")
				return RunResult{
					OK:     false,
					Output: log.String(),
					Creds:  creds,
					Step:   step.Name,
					Err:    fmt.Errorf("%s: %s", step.Name, msg),
				}
			}
			report(fmt.Sprintf("%s (%d/%d)", step.Name, done, total))
		}
	}
	if onProgress != nil {
		onProgress(1, mat.Name+" complete")
	}
	return RunResult{OK: true, Output: log.String(), Creds: creds}
}

func (r Runner) runCommand(cmd string, apt bool, note func(string)) (*ssh.ExecResult, error) {
	wrapped := r.remoteShell(cmd)
	if !apt {
		return r.Exec.Run(wrapped)
	}

	for attempt := 0; attempt <= aptLockMaxRetries; attempt++ {
		if err := r.waitAptLock(note); err != nil {
			return &ssh.ExecResult{ExitCode: 1, Stderr: err.Error()}, nil
		}
		res, err := r.Exec.Run(wrapped)
		if err != nil {
			return nil, err
		}
		out := res.Stdout + "\n" + res.Stderr
		if res.ExitCode == 0 || !isAptLockError(out) {
			return res, nil
		}
		if attempt == aptLockMaxRetries {
			return res, nil
		}
		note(fmt.Sprintf("apt lock contended (unattended-upgrades or another apt); retry %d/%d…",
			attempt+1, aptLockMaxRetries))
		time.Sleep(time.Duration(aptLockWaitSecs) * time.Second)
	}
	return r.Exec.Run(wrapped)
}

func (r Runner) waitAptLock(note func(string)) error {
	// Remote poll: locks held by unattended-upgrades / apt-get / dpkg.
	script := `
held=0
for lock in /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/lib/apt/lists/lock /var/cache/apt/archives/lock; do
  if command -v fuser >/dev/null 2>&1; then
    if fuser "$lock" >/dev/null 2>&1; then held=1; break; fi
  elif [ -f "$lock" ] && command -v lsof >/dev/null 2>&1; then
    if lsof "$lock" >/dev/null 2>&1; then held=1; break; fi
  fi
done
if [ "$held" = "1" ]; then
  pid=$(fuser /var/lib/dpkg/lock-frontend 2>/dev/null | tr -d ' ' | head -c 32 || true)
  echo "BUSY:${pid:-unknown}"
  exit 2
fi
echo FREE
exit 0
`
	for i := 0; i < aptLockMaxWaits; i++ {
		res, err := r.Exec.Run(r.remoteShell(script))
		if err != nil {
			return err
		}
		out := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
		if res.ExitCode == 0 && strings.Contains(out, "FREE") {
			return nil
		}
		pid := "unknown"
		if _, rest, ok := strings.Cut(out, "BUSY:"); ok {
			pid = strings.TrimSpace(strings.Split(rest, "\n")[0])
		}
		if note != nil && i%2 == 0 {
			note(fmt.Sprintf("Waiting for apt/dpkg lock (pid %s)… %ds", pid, (i+1)*aptLockWaitSecs))
		}
		time.Sleep(time.Duration(aptLockWaitSecs) * time.Second)
	}
	return fmt.Errorf("timed out waiting for apt/dpkg lock after %d seconds", aptLockMaxWaits*aptLockWaitSecs)
}

func usesApt(cmd string) bool {
	c := strings.ToLower(cmd)
	for _, needle := range []string{
		"apt-get ", "apt-get\t", "apt-get\n",
		"apt ", "\napt ",
		"dpkg ",
		"unattended-upgrade",
	} {
		if strings.Contains(c, needle) {
			return true
		}
	}
	// Common patterns without trailing space in scripts
	return strings.Contains(c, "apt-get") || strings.Contains(c, "apt install") || strings.Contains(c, "apt update")
}

func isAptLockError(output string) bool {
	s := strings.ToLower(output)
	return strings.Contains(s, "could not get lock") ||
		strings.Contains(s, "unable to acquire the dpkg frontend lock") ||
		strings.Contains(s, "unable to lock the administration directory") ||
		strings.Contains(s, "is another process using it") ||
		strings.Contains(s, "dpkg frontend lock")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
