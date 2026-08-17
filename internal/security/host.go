package security

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/hostfirewall"
	"github.com/lyracorp/xmanager/internal/proxy"
	"github.com/lyracorp/xmanager/internal/ssh"
)

// FirewallRule is a parsed ufw or iptables row.
type FirewallRule struct {
	Port   string
	Proto  string
	Action string
	From   string
}

// FirewallStatus summarizes host firewall state.
type FirewallStatus struct {
	Backend string // ufw | iptables
	Active  bool
	Rules   []FirewallRule
	Raw     string
}

// SSHConfig holds effective sshd settings.
type SSHConfig struct {
	Port                   string
	PermitRootLogin        string
	PasswordAuthentication string
	PubkeyAuthentication   string
	Raw                    string
}

// SSLCert is a parsed certbot certificate entry.
type SSLCert struct {
	Name     string
	Domains  []string
	Expiry   string
	CertPath string
}

// Fail2banStatus summarizes fail2ban jails.
type Fail2banStatus struct {
	Installed bool
	Jails     []Fail2banJail
	Raw       string
}

// Fail2banJail is one jail with banned IPs.
type Fail2banJail struct {
	Name            string
	Banned          []string
	CurrentlyBanned int
}

// AuthFail is a parsed SSH login failure line.
type AuthFail struct {
	Time   string
	User   string
	IP     string
	Detail string
}

// FirewallStatus reads ufw or iptables state.
func ReadFirewall(exec *ssh.Executor) FirewallStatus {
	if exec == nil {
		return FirewallStatus{Backend: "none"}
	}
	if hasUFW(exec) {
		raw := exec.RunQuiet("sudo ufw status verbose 2>/dev/null")
		st := ParseUFWStatus(raw)
		st.Backend = "ufw"
		st.Raw = raw
		return st
	}
	raw := exec.RunQuiet("sudo iptables -L INPUT -n -v 2>/dev/null")
	return FirewallStatus{
		Backend: "iptables",
		Active:  strings.TrimSpace(raw) != "",
		Rules:   ParseIPTables(raw),
		Raw:     raw,
	}
}

func hasUFW(exec *ssh.Executor) bool {
	return strings.TrimSpace(exec.RunQuiet("command -v ufw 2>/dev/null")) != ""
}

// AllowPorts opens inbound ports via hostfirewall.
func AllowPorts(exec *ssh.Executor, portsSpec string) error {
	ports, proto, err := hostfirewall.ParsePortsList(portsSpec)
	if err != nil {
		return err
	}
	return hostfirewall.Allow(ports, proto)
}

// DenyPort closes an inbound port.
func DenyPort(exec *ssh.Executor, port int, proto string, protected map[int]string) error {
	return hostfirewall.Deny(port, proto, protected)
}

// SSHConfig reads effective sshd settings.
func SSHConfigRead(exec *ssh.Executor) SSHConfig {
	if exec == nil {
		return SSHConfig{}
	}
	raw := exec.RunQuiet("sudo sshd -T 2>/dev/null")
	if strings.TrimSpace(raw) == "" {
		raw = exec.RunQuiet("grep -E '^(Port|PermitRootLogin|PasswordAuthentication|PubkeyAuthentication)' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/*.conf 2>/dev/null")
	}
	cfg := SSHConfig{Raw: raw}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		switch parts[0] {
		case "port":
			cfg.Port = parts[1]
		case "permitrootlogin":
			cfg.PermitRootLogin = parts[1]
		case "passwordauthentication":
			cfg.PasswordAuthentication = parts[1]
		case "pubkeyauthentication":
			cfg.PubkeyAuthentication = parts[1]
		}
	}
	if cfg.Port == "" {
		cfg.Port = "22"
	}
	return cfg
}

// ApplySSHHardening applies wizard-style sshd hardening.
func ApplySSHHardening(exec *ssh.Executor) error {
	if exec == nil {
		return fmt.Errorf("no executor")
	}
	cmds := []string{
		`sudo sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config`,
		`sudo sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config`,
		`sudo systemctl reload sshd 2>/dev/null || sudo systemctl reload ssh 2>/dev/null || sudo service ssh reload 2>/dev/null || true`,
	}
	for _, c := range cmds {
		if res := exec.RunQuiet(c); strings.Contains(strings.ToLower(res), "error") {
			// best effort
		}
	}
	return nil
}

// SSLCerts lists certbot certificates and nginx vhosts.
func SSLCerts(exec *ssh.Executor) ([]SSLCert, []proxy.VHost, error) {
	if exec == nil {
		return nil, nil, fmt.Errorf("no executor")
	}
	raw := exec.RunQuiet("sudo certbot certificates 2>/dev/null")
	certs := ParseCertbot(raw)
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr == nil {
		return certs, nil, nil
	}
	vhosts, err := mgr.ListVHosts()
	return certs, vhosts, err
}

// RenewSSL runs certbot renew.
func RenewSSL(exec *ssh.Executor) (string, error) {
	mgr := proxy.NewManager(proxy.Nginx, exec)
	if mgr == nil {
		return "", fmt.Errorf("no nginx manager")
	}
	ng, ok := mgr.(*proxy.NginxManager)
	if !ok {
		return "", fmt.Errorf("nginx unavailable")
	}
	return ng.RenewSSL()
}

// ReadFail2ban reads fail2ban jails.
func ReadFail2ban(exec *ssh.Executor) Fail2banStatus {
	if exec == nil {
		return Fail2banStatus{}
	}
	if strings.TrimSpace(exec.RunQuiet("command -v fail2ban-client 2>/dev/null")) == "" {
		return Fail2banStatus{Installed: false}
	}
	raw := exec.RunQuiet("sudo fail2ban-client status 2>/dev/null")
	st := ParseFail2ban(raw)
	st.Installed = true
	st.Raw = raw
	for i := range st.Jails {
		jraw := exec.RunQuiet("sudo fail2ban-client status " + shellSafe(st.Jails[i].Name) + " 2>/dev/null")
		banned := ParseFail2banBanned(jraw)
		st.Jails[i].Banned = banned
		st.Jails[i].CurrentlyBanned = len(banned)
	}
	return st
}

// UnbanIP removes an IP from a fail2ban jail.
func UnbanIP(exec *ssh.Executor, jail, ip string) error {
	if exec == nil {
		return fmt.Errorf("no executor")
	}
	jail = strings.TrimSpace(jail)
	ip = strings.TrimSpace(ip)
	if jail == "" || ip == "" {
		return fmt.Errorf("jail and ip required")
	}
	out := exec.RunQuiet(fmt.Sprintf("sudo fail2ban-client set %s unbanip %s 2>&1", shellSafe(jail), shellSafe(ip)))
	if strings.Contains(strings.ToLower(out), "error") {
		return fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return nil
}

// AuthFailures tails SSH auth logs and parses failure lines.
func AuthFailures(exec *ssh.Executor, lines int) []AuthFail {
	if exec == nil {
		return nil
	}
	if lines <= 0 {
		lines = 100
	}
	var text string
	for _, cmd := range []string{
		fmt.Sprintf("journalctl -u ssh -u sshd -n %d --no-pager 2>/dev/null", lines),
		fmt.Sprintf("tail -n %d /var/log/auth.log 2>/dev/null", lines),
		fmt.Sprintf("tail -n %d /var/log/secure 2>/dev/null", lines),
	} {
		text = exec.RunQuiet(cmd)
		if strings.TrimSpace(text) != "" {
			break
		}
	}
	return ParseAuthFailures(text)
}

func shellSafe(s string) string {
	return strings.NewReplacer("'", "", ";", "", "&", "", "|", "", "`", "").Replace(s)
}
