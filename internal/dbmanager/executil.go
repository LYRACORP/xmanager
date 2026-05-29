package dbmanager

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

func requireOK(result *ssh.ExecResult, err error, hint string) error {
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("%s", hint)
	}
	if result.ExitCode != 0 {
		msg := strings.TrimSpace(result.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(result.Stdout)
		}
		if msg == "" {
			msg = hint
		}
		return fmt.Errorf("%s (exit %d)", msg, result.ExitCode)
	}
	return nil
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}
