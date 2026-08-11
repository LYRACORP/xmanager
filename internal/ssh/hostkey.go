package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ErrHostKeyMismatch is returned (via errors.Is) when the remote host key
// does not match an existing ~/.ssh/known_hosts entry.
var ErrHostKeyMismatch = errors.New("ssh: host key mismatch")

// HostKeyMismatchError provides details for interactive replacement prompts.
type HostKeyMismatchError struct {
	Host           string
	KnownHostsPath string
	RemoteKey      ssh.PublicKey
}

func (e *HostKeyMismatchError) Error() string {
	fp := ""
	if e.RemoteKey != nil {
		fp = " new fingerprint " + ssh.FingerprintSHA256(e.RemoteKey)
	}
	return fmt.Sprintf("host key mismatch for %s (remote key changed;%s confirm to replace entry in %s)",
		e.Host, fp, e.KnownHostsPath)
}

func (e *HostKeyMismatchError) Is(target error) bool {
	return target == ErrHostKeyMismatch
}

// Fingerprint returns the SHA256 fingerprint of the remote key, if known.
func (e *HostKeyMismatchError) Fingerprint() string {
	if e == nil || e.RemoteKey == nil {
		return ""
	}
	return ssh.FingerprintSHA256(e.RemoteKey)
}

// IsHostKeyMismatch reports whether err is (or wraps) a host key mismatch.
func IsHostKeyMismatch(err error) bool {
	return errors.Is(err, ErrHostKeyMismatch)
}

// AsHostKeyMismatch extracts a HostKeyMismatchError from err, if present.
func AsHostKeyMismatch(err error) (*HostKeyMismatchError, bool) {
	var target *HostKeyMismatchError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// knownHostsCallback verifies host keys against ~/.ssh/known_hosts.
// Unknown hosts are accepted on first connect (TOFU) and appended to the file,
// matching typical OpenSSH interactive behavior. Changed keys fail unless
// replaceChanged is true (after explicit user confirmation).
func knownHostsCallback(replaceChanged bool) (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	khPath := filepath.Join(home, ".ssh", "known_hosts")

	if err := os.MkdirAll(filepath.Dir(khPath), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(khPath); os.IsNotExist(err) {
		if f, createErr := os.OpenFile(khPath, os.O_CREATE|os.O_WRONLY, 0600); createErr == nil {
			_ = f.Close()
		}
	}

	base, err := knownhosts.New(khPath)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if !errors.As(err, &keyErr) {
			return err
		}

		// Known host but key mismatch — require confirmation before replacing.
		if len(keyErr.Want) > 0 {
			if replaceChanged {
				if repErr := replaceKnownHostEntries(khPath, keyErr.Want, hostname, remote, key); repErr != nil {
					return fmt.Errorf("replacing host key for %s: %w", hostname, repErr)
				}
				return nil
			}
			return &HostKeyMismatchError{
				Host:           hostname,
				KnownHostsPath: khPath,
				RemoteKey:      key,
			}
		}

		// Unknown host — trust on first use and persist.
		if appendErr := appendKnownHost(khPath, hostname, remote, key); appendErr != nil {
			return fmt.Errorf("accepting new host key: %w", appendErr)
		}
		return nil
	}, nil
}

func appendKnownHost(khPath, hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	addr := []string{hostname}
	if remote != nil {
		if host, _, splitErr := net.SplitHostPort(remote.String()); splitErr == nil && host != "" {
			addr = []string{knownhosts.Normalize(remote.String())}
		}
	}
	line := knownhosts.Line(addr, key)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}

// replaceKnownHostEntries removes mismatched known_hosts lines and appends the new key.
func replaceKnownHostEntries(khPath string, want []knownhosts.KnownKey, hostname string, remote net.Addr, key ssh.PublicKey) error {
	skip := make(map[int]struct{})
	for _, k := range want {
		if k.Filename == "" || k.Filename == khPath {
			if k.Line > 0 {
				skip[k.Line] = struct{}{}
			}
		}
	}
	if len(skip) > 0 {
		if err := removeKnownHostsLines(khPath, skip); err != nil {
			return err
		}
	}
	return appendKnownHost(khPath, hostname, remote, key)
}

// removeKnownHostsLines rewrites khPath omitting 1-based line numbers in skip.
func removeKnownHostsLines(khPath string, skip map[int]struct{}) error {
	f, err := os.Open(khPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var keep []string
	sc := bufio.NewScanner(f)
	// known_hosts lines can be long (certificates); raise the limit.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if _, drop := skip[lineNo]; drop {
			continue
		}
		keep = append(keep, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(khPath), "known_hosts.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	out := strings.Join(keep, "\n")
	if len(keep) > 0 {
		out += "\n"
	}
	if _, err := tmp.WriteString(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, khPath)
}
