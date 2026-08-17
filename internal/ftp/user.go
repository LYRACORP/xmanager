package ftp

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	GroupName  = "xmanager-ftp"
	HomeRoot   = "/srv/ftp"
	Userlist   = "/etc/vsftpd.userlist"
	ConfigPath = "/etc/vsftpd.conf"
	Nologin    = "/usr/sbin/nologin"
	PasvMin    = 40000
	PasvMax    = 40100
)

var usernameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

var reservedUsers = map[string]bool{
	"root": true, "ubuntu": true, "www-data": true, "daemon": true, "nobody": true,
	"sync": true, "sys": true, "bin": true, "admin": true, "ftp": true, "xmanager": true,
	"mysql": true, "postgres": true, "nginx": true, "sshd": true, "messagebus": true,
	"systemd-network": true, "systemd-resolve": true, "backup": true, "list": true,
}

func DefaultHome(username string) string {
	return path.Join(HomeRoot, username)
}

func ValidateUsername(name string) error {
	name = strings.TrimSpace(name)
	if !usernameRe.MatchString(name) {
		return fmt.Errorf("username must be 3–32 chars: lowercase letter, then letters/digits/_")
	}
	if reservedUsers[name] {
		return fmt.Errorf("username %s is reserved", name)
	}
	return nil
}

func ValidateHome(username, home string) (string, error) {
	if err := ValidateUsername(username); err != nil {
		return "", err
	}
	home = strings.TrimSpace(home)
	if home == "" {
		home = DefaultHome(username)
	}
	home = path.Clean(home)
	if !strings.HasPrefix(home, "/") {
		return "", fmt.Errorf("home must be an absolute path")
	}
	if home == HomeRoot || home == HomeRoot+"/" {
		return "", fmt.Errorf("home must be a subdirectory of %s", HomeRoot)
	}
	prefix := HomeRoot + "/"
	if home != prefix+path.Base(home) && !strings.HasPrefix(home, prefix) {
		return "", fmt.Errorf("home must be under %s", HomeRoot)
	}
	if strings.Contains(home, "..") {
		return "", fmt.Errorf("invalid home path")
	}
	return home, nil
}

func parseUserlist(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

func setUserlistEnabled(names []string, user string, enabled bool) []string {
	var out []string
	found := false
	for _, n := range names {
		if n == user {
			found = true
			if enabled {
				out = append(out, n)
			}
			continue
		}
		out = append(out, n)
	}
	if enabled && !found {
		out = append(out, user)
	}
	return out
}

func formatUserlist(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, "\n") + "\n"
}
