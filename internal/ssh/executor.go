package ssh

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor struct {
	client *Client
}

func NewExecutor(client *Client) *Executor {
	return &Executor{client: client}
}

func (e *Executor) Run(cmd string) (*ExecResult, error) {
	session, err := e.client.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	exitCode := 0
	if err := session.Run(cmd); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
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

func (e *Executor) RunCombined(cmd string) (string, error) {
	result, err := e.Run(cmd)
	if err != nil {
		return "", err
	}
	output := result.Stdout
	if result.Stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += result.Stderr
	}
	return output, nil
}

func (e *Executor) Stream(cmd string) (io.Reader, func(), error) {
	session, err := e.client.conn.NewSession()
	if err != nil {
		return nil, nil, fmt.Errorf("creating session: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("getting stdout pipe: %w", err)
	}

	if err := session.Start(cmd); err != nil {
		session.Close()
		return nil, nil, fmt.Errorf("starting command: %w", err)
	}

	cleanup := func() {
		session.Signal(ssh.SIGINT)
		session.Close()
	}

	return stdout, cleanup, nil
}

func (e *Executor) RunQuiet(cmd string) string {
	result, err := e.Run(cmd)
	if err != nil || result.ExitCode != 0 {
		return ""
	}
	return result.Stdout
}

func (e *Executor) RunAll(cmds []string) (map[string]*ExecResult, error) {
	results := make(map[string]*ExecResult, len(cmds))
	for _, cmd := range cmds {
		result, err := e.Run(cmd)
		if err != nil {
			results[cmd] = &ExecResult{Stderr: err.Error(), ExitCode: -1}
			continue
		}
		results[cmd] = result
	}
	return results, nil
}
