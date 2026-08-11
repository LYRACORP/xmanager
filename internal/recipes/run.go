package recipes

import (
	"fmt"
	"strings"

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
	Exec *ssh.Executor
}

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
	for _, step := range mat.Steps {
		report(step.Name)
		log.WriteString("==> " + step.Name + "\n")
		for _, cmd := range step.Commands {
			detail := truncate(strings.ReplaceAll(cmd, "\n", " "), 72)
			report(step.Name + ": " + detail)
			res, err := r.Exec.Run("bash -lc " + shellQuote(cmd))
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
