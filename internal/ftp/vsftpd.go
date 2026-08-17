package ftp

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const vsftpdConf = `listen=YES
listen_ipv6=NO
anonymous_enable=NO
local_enable=YES
write_enable=YES
local_umask=022
dirmessage_enable=YES
use_localtime=YES
xferlog_enable=YES
connect_from_port_20=YES
chroot_local_user=YES
allow_writeable_chroot=YES
secure_chroot_dir=/var/run/vsftpd/empty
pam_service_name=vsftpd
userlist_enable=YES
userlist_file=/etc/vsftpd.userlist
userlist_deny=NO
pasv_enable=YES
pasv_min_port=40000
pasv_max_port=40100
ssl_enable=NO
idle_session_timeout=600
data_connection_timeout=120
seccomp_sandbox=NO
`

type Manager struct {
	exec *ssh.Executor
}

func New(exec *ssh.Executor) *Manager {
	return &Manager{exec: exec}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (m *Manager) run(cmd string) error {
	if m.exec == nil {
		return fmt.Errorf("no executor")
	}
	res, err := m.exec.Run(cmd)
	if err != nil {
		return err
	}
	if res != nil && res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
		if msg == "" {
			msg = fmt.Sprintf("exit %d", res.ExitCode)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (m *Manager) writeFile(path, content string) error {
	cmd := fmt.Sprintf("sudo tee %s > /dev/null << 'XMEOF'\n%s\nXMEOF", path, strings.TrimRight(content, "\n"))
	return m.run(cmd)
}

func (m *Manager) IsEnabled() bool {
	if m.exec == nil {
		return false
	}
	out := strings.ToLower(strings.TrimSpace(m.exec.RunQuiet("systemctl is-active vsftpd 2>/dev/null || systemctl is-active vsftpd.service 2>/dev/null")))
	return out == "active"
}

func (m *Manager) EnsureInstalled() error {
	if m.exec == nil {
		return fmt.Errorf("no executor")
	}
	if m.exec.RunQuiet("command -v vsftpd 2>/dev/null") != "" {
		return nil
	}
	if err := m.run("sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq vsftpd 2>&1"); err != nil {
		return fmt.Errorf("install vsftpd: %w", err)
	}
	if m.exec.RunQuiet("command -v vsftpd 2>/dev/null") == "" {
		return fmt.Errorf("vsftpd still not found after apt install")
	}
	return nil
}

func (m *Manager) writeConfig() error {
	_ = m.run("sudo mkdir -p /var/run/vsftpd/empty /srv/ftp")
	if err := m.writeFile(ConfigPath, vsftpdConf); err != nil {
		return fmt.Errorf("write vsftpd.conf: %w", err)
	}
	return nil
}

func (m *Manager) WriteUserlist(usernames []string) error {
	body := formatUserlist(usernames)
	if body == "" {
		body = "\n"
	}
	return m.writeFile(Userlist, body)
}

func (m *Manager) readUserlist() []string {
	if m.exec == nil {
		return nil
	}
	raw := m.exec.RunQuiet("sudo cat " + Userlist + " 2>/dev/null")
	return parseUserlist(raw)
}

func (m *Manager) reload() {
	_ = m.run("sudo systemctl reload vsftpd 2>/dev/null || sudo systemctl restart vsftpd 2>/dev/null || true")
}

func (m *Manager) openFirewall() {
	_ = m.run("sudo ufw allow 21/tcp >/dev/null 2>&1 || sudo iptables -C INPUT -p tcp --dport 21 -j ACCEPT 2>/dev/null || sudo iptables -I INPUT -p tcp --dport 21 -j ACCEPT")
	_ = m.run("sudo ufw allow 40000:40100/tcp >/dev/null 2>&1 || sudo iptables -C INPUT -p tcp --dport 40000:40100 -j ACCEPT 2>/dev/null || sudo iptables -I INPUT -p tcp --dport 40000:40100 -j ACCEPT")
}

func (m *Manager) closeFirewall() {
	_ = m.run("sudo ufw --force delete allow 21/tcp >/dev/null 2>&1; sudo ufw deny 21/tcp >/dev/null 2>&1 || true")
	_ = m.run("sudo ufw --force delete allow 40000:40100/tcp >/dev/null 2>&1; sudo ufw deny 40000:40100/tcp >/dev/null 2>&1 || true")
	_ = m.run("sudo iptables -D INPUT -p tcp --dport 21 -j ACCEPT 2>/dev/null; sudo iptables -C INPUT -p tcp --dport 21 -j REJECT 2>/dev/null || sudo iptables -I INPUT -p tcp --dport 21 -j REJECT 2>/dev/null || true")
	_ = m.run("sudo iptables -D INPUT -p tcp --dport 40000:40100 -j ACCEPT 2>/dev/null; sudo iptables -C INPUT -p tcp --dport 40000:40100 -j REJECT 2>/dev/null || sudo iptables -I INPUT -p tcp --dport 40000:40100 -j REJECT 2>/dev/null || true")
}

// Enable installs vsftpd, writes jail config + allow-list, starts the unit, opens ports.
func (m *Manager) Enable(allowedUsers []string) error {
	if err := m.EnsureInstalled(); err != nil {
		return err
	}
	if err := m.writeConfig(); err != nil {
		return err
	}
	if err := m.WriteUserlist(allowedUsers); err != nil {
		return fmt.Errorf("userlist: %w", err)
	}
	if err := m.run("sudo systemctl enable --now vsftpd 2>&1 || sudo systemctl enable --now vsftpd.service 2>&1"); err != nil {
		return fmt.Errorf("start vsftpd: %w", err)
	}
	m.openFirewall()
	if !m.IsEnabled() {
		return fmt.Errorf("vsftpd did not become active")
	}
	return nil
}

// Disable stops vsftpd and closes FTP ports. User accounts remain.
func (m *Manager) Disable() error {
	_ = m.run("sudo systemctl disable --now vsftpd 2>/dev/null || sudo systemctl disable --now vsftpd.service 2>/dev/null || true")
	m.closeFirewall()
	return nil
}

func (m *Manager) ensureGroup() error {
	return m.run("sudo groupadd -f " + GroupName)
}

func (m *Manager) setPassword(username, password string) error {
	if strings.ContainsAny(password, "\n\r") {
		return fmt.Errorf("invalid password")
	}
	if password == "" {
		return fmt.Errorf("password required")
	}
	pair := username + ":" + password
	return m.run(fmt.Sprintf("printf '%%s\\n' %s | sudo chpasswd", shellQuote(pair)))
}

func (m *Manager) userExists(username string) bool {
	out := m.exec.RunQuiet("id -u " + shellQuote(username) + " 2>/dev/null")
	return strings.TrimSpace(out) != ""
}

func (m *Manager) inFTPGroup(username string) bool {
	out := m.exec.RunQuiet("id -nG " + shellQuote(username) + " 2>/dev/null")
	for _, g := range strings.Fields(out) {
		if g == GroupName {
			return true
		}
	}
	return false
}

func (m *Manager) prepareHome(username, home string) error {
	if err := m.run("sudo mkdir -p " + shellQuote(home)); err != nil {
		return err
	}
	if err := m.run(fmt.Sprintf("sudo chown %s:%s %s", shellQuote(username), GroupName, shellQuote(home))); err != nil {
		return err
	}
	return m.run("sudo chmod 755 " + shellQuote(home))
}

// CreateUser adds a nologin, jailed system account and allow-lists it for vsftpd.
func (m *Manager) CreateUser(username, password, home string) (string, error) {
	if err := ValidateUsername(username); err != nil {
		return "", err
	}
	home, err := ValidateHome(username, home)
	if err != nil {
		return "", err
	}
	if err := m.ensureGroup(); err != nil {
		return "", fmt.Errorf("group: %w", err)
	}
	if m.userExists(username) {
		if !m.inFTPGroup(username) {
			return "", fmt.Errorf("user %s already exists and is not an FTP account", username)
		}
	} else {
		cmd := fmt.Sprintf("sudo useradd -g %s -d %s -s %s -M %s",
			GroupName, shellQuote(home), Nologin, shellQuote(username))
		if err := m.run(cmd); err != nil {
			return "", fmt.Errorf("useradd: %w", err)
		}
	}
	if err := m.prepareHome(username, home); err != nil {
		return "", fmt.Errorf("home: %w", err)
	}
	if err := m.setPassword(username, password); err != nil {
		return "", err
	}
	names := setUserlistEnabled(m.readUserlist(), username, true)
	if err := m.WriteUserlist(names); err != nil {
		return "", err
	}
	if m.IsEnabled() {
		m.reload()
	}
	return home, nil
}

func (m *Manager) SetPassword(username, password string) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if !m.userExists(username) {
		return fmt.Errorf("user %s not found", username)
	}
	if err := m.setPassword(username, password); err != nil {
		return err
	}
	return nil
}

func (m *Manager) SetEnabled(username string, enabled bool) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	names := setUserlistEnabled(m.readUserlist(), username, enabled)
	if err := m.WriteUserlist(names); err != nil {
		return err
	}
	if m.IsEnabled() {
		m.reload()
	}
	return nil
}

func (m *Manager) DeleteUser(username string, removeHome bool) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	names := setUserlistEnabled(m.readUserlist(), username, false)
	_ = m.WriteUserlist(names)
	flag := ""
	if removeHome {
		flag = "-r "
	}
	_ = m.run("sudo userdel " + flag + shellQuote(username) + " 2>/dev/null || true")
	if m.IsEnabled() {
		m.reload()
	}
	return nil
}
