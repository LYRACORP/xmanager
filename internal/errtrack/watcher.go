package errtrack

import (
	"bufio"
	"regexp"
	"strings"
	"sync"

	"github.com/lyracorp/xmanager/internal/notify"
	"github.com/lyracorp/xmanager/internal/ssh"
)

var errorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(error|err|fatal|panic|exception|fail(ed|ure)?)\b`),
	regexp.MustCompile(`(?i)\b(critical|crit|emergency|alert)\b`),
	regexp.MustCompile(`(?i)(segfault|segmentation fault|out of memory|oom|killed)`),
	regexp.MustCompile(`(?i)(permission denied|access denied|unauthorized|forbidden)`),
	regexp.MustCompile(`(?i)(connection refused|timeout|timed out|unreachable)`),
}

type Watcher struct {
	tracker   *Tracker
	notifier  notify.Notifier
	serverID  uint
	serverName string
	mu        sync.Mutex
	cancel    []func()
}

func NewWatcher(tracker *Tracker, notifier notify.Notifier, serverID uint, serverName string) *Watcher {
	return &Watcher{
		tracker:    tracker,
		notifier:   notifier,
		serverID:   serverID,
		serverName: serverName,
	}
}

func (w *Watcher) Watch(exec *ssh.Executor, service, command string) error {
	reader, cleanup, err := exec.Stream(command)
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.cancel = append(w.cancel, cleanup)
	w.mu.Unlock()

	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			if severity := detectSeverity(line); severity != "" {
				if err := w.tracker.Record(w.serverID, service, line, "", severity); err != nil {
					// ignore or log
				}

				if w.notifier != nil && (severity == "error" || severity == "critical") {
					if err := w.notifier.Send(notify.Alert{
						ServerName: w.serverName,
						Service:    service,
						Title:      strings.ToUpper(severity) + " detected",
						Message:    truncate(line, 500),
						Severity:   notify.Severity(severity),
					}); err != nil {
						// ignore or log
					}
				}
			}
		}
	}()

	return nil
}

func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, cancel := range w.cancel {
		cancel()
	}
	w.cancel = nil
}

func detectSeverity(line string) string {
	lower := strings.ToLower(line)

	if strings.Contains(lower, "critical") || strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "panic") || strings.Contains(lower, "emergency") {
		return "critical"
	}

	for _, p := range errorPatterns {
		if p.MatchString(line) {
			if strings.Contains(lower, "warn") {
				return "warning"
			}
			return "error"
		}
	}

	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
