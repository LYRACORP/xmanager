package ssh

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor struct {
	client *Client
	local  bool
}

func NewExecutor(client *Client) *Executor {
	return &Executor{client: client}
}

func (e *Executor) Run(cmd string) (*ExecResult, error) {
	if e.local {
		return e.runLocal(cmd)
	}
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

// StreamWait runs cmd, calling onLine for each stdout/stderr line, and returns
// after the process exits. ctx cancel kills the command.
func (e *Executor) StreamWait(ctx context.Context, cmd string, onLine func(string)) error {
	if e == nil {
		return fmt.Errorf("nil executor")
	}
	if e.local {
		return e.streamWaitLocal(ctx, cmd, onLine)
	}
	return e.streamWaitRemote(ctx, cmd, onLine)
}

func (e *Executor) Stream(cmd string) (io.Reader, func(), error) {
	if e.local {
		c := exec.Command("bash", "-c", cmd)
		stdout, err := c.StdoutPipe()
		if err != nil {
			return nil, nil, err
		}
		if err := c.Start(); err != nil {
			return nil, nil, err
		}
		cleanup := func() {
			_ = c.Process.Kill()
			_ = c.Wait()
		}
		return stdout, cleanup, nil
	}
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
		_ = session.Signal(ssh.SIGINT)
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

func (e *Executor) UnderlyingClient() *Client {
	if e == nil {
		return nil
	}
	return e.client
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

func (e *Executor) streamWaitLocal(ctx context.Context, cmd string, onLine func(string)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	c := exec.CommandContext(ctx, "bash", "-c", cmd)
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	c.Stderr = c.Stdout
	if err := c.Start(); err != nil {
		return err
	}
	scanLines(stdout, onLine)
	return c.Wait()
}

func (e *Executor) streamWaitRemote(ctx context.Context, cmd string, onLine func(string)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if e.client == nil || e.client.conn == nil {
		return fmt.Errorf("not connected")
	}
	session, err := e.client.conn.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("getting stdout pipe: %w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("getting stderr pipe: %w", err)
	}
	if err := session.Start(cmd); err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); scanLines(stdout, onLine) }()
		go func() { defer wg.Done(); scanLines(stderr, onLine) }()
		wg.Wait()
		close(done)
	}()

	waitErr := make(chan error, 1)
	go func() { waitErr <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGINT)
		session.Close()
		<-done
		return ctx.Err()
	case err := <-waitErr:
		<-done
		return err
	}
}

func scanLines(r io.Reader, onLine func(string)) {
	if onLine == nil {
		_, _ = io.Copy(io.Discard, r)
		return
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		onLine(sc.Text())
	}
}
