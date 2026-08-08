package ssh

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// NewLocalExecutor returns an Executor that runs commands on the local host
// (used by the node web panel). Managers that take *Executor work unchanged.
func NewLocalExecutor() *Executor {
	return &Executor{local: true}
}

func (e *Executor) IsLocal() bool {
	return e != nil && e.local
}

func (e *Executor) runLocal(cmd string) (*ExecResult, error) {
	c := exec.Command("bash", "-c", cmd)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if status, ok := ee.Sys().(syscall.WaitStatus); ok {
				exitCode = status.ExitStatus()
			} else {
				exitCode = 1
			}
		} else {
			return nil, fmt.Errorf("running command: %w", err)
		}
	}
	return &ExecResult{
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
		ExitCode: exitCode,
	}, nil
}
