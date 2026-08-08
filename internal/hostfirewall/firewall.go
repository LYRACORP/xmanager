package hostfirewall

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ProtectedPorts must never be closed via the panel (lock-out risk).
var ProtectedPorts = map[int]string{
	22: "SSH",
}

// Allow opens inbound access for each port (ufw, else iptables).
func Allow(ports []int, proto string) error {
	proto = normalizeProto(proto)
	if len(ports) == 0 {
		return fmt.Errorf("no ports specified")
	}
	for _, p := range ports {
		if err := validatePort(p); err != nil {
			return err
		}
	}
	if hasUFW() {
		var errs []string
		for _, p := range ports {
			if err := run("ufw", "allow", fmt.Sprintf("%d/%s", p, proto)); err != nil {
				errs = append(errs, fmt.Sprintf("%d: %v", p, err))
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("ufw allow: %s", strings.Join(errs, "; "))
		}
		return nil
	}
	for _, p := range ports {
		if err := iptablesAllow(p, proto); err != nil {
			return err
		}
	}
	return nil
}

// Deny closes inbound access for a port (ufw deny + delete allow, else iptables REJECT).
func Deny(port int, proto string, protectedExtra map[int]string) error {
	proto = normalizeProto(proto)
	if err := validatePort(port); err != nil {
		return err
	}
	if reason, ok := ProtectedPorts[port]; ok {
		return fmt.Errorf("refusing to close port %d (%s)", port, reason)
	}
	if protectedExtra != nil {
		if reason, ok := protectedExtra[port]; ok {
			return fmt.Errorf("refusing to close port %d (%s)", port, reason)
		}
	}

	if hasUFW() {
		_ = run("ufw", "--force", "delete", "allow", fmt.Sprintf("%d/%s", port, proto))
		if err := run("ufw", "deny", fmt.Sprintf("%d/%s", port, proto)); err != nil {
			return fmt.Errorf("ufw deny %d/%s: %w", port, proto, err)
		}
		return nil
	}
	return iptablesDeny(port, proto)
}

// ParsePortsList accepts "80", "80,443", "8080 3000", "80/tcp".
func ParsePortsList(s string) ([]int, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, "", fmt.Errorf("ports required")
	}
	proto := "tcp"
	var ports []int
	seen := map[int]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pProto := proto
		if i := strings.IndexByte(part, '/'); i >= 0 {
			pProto = normalizeProto(part[i+1:])
			part = part[:i]
			proto = pProto
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, "", fmt.Errorf("invalid port %q", part)
		}
		if err := validatePort(n); err != nil {
			return nil, "", err
		}
		if !seen[n] {
			seen[n] = true
			ports = append(ports, n)
		}
	}
	if len(ports) == 0 {
		return nil, "", fmt.Errorf("ports required")
	}
	return ports, proto, nil
}

func validatePort(p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("port %d out of range", p)
	}
	return nil
}

func normalizeProto(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if p == "udp" {
		return "udp"
	}
	return "tcp"
}

func hasUFW() bool {
	path, err := exec.LookPath("ufw")
	return err == nil && path != ""
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func iptablesAllow(port int, proto string) error {
	spec := []string{"-C", "INPUT", "-p", proto, "--dport", strconv.Itoa(port), "-j", "ACCEPT"}
	if run("iptables", spec...) == nil {
		return nil
	}
	return run("iptables", "-I", "INPUT", "-p", proto, "--dport", strconv.Itoa(port), "-j", "ACCEPT")
}

func iptablesDeny(port int, proto string) error {
	// Drop any existing ACCEPT for this port, then reject.
	_ = run("iptables", "-D", "INPUT", "-p", proto, "--dport", strconv.Itoa(port), "-j", "ACCEPT")
	spec := []string{"-C", "INPUT", "-p", proto, "--dport", strconv.Itoa(port), "-j", "REJECT"}
	if run("iptables", spec...) == nil {
		return nil
	}
	return run("iptables", "-I", "INPUT", "-p", proto, "--dport", strconv.Itoa(port), "-j", "REJECT")
}
