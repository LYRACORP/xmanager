package docker

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

const (
	RoleWorker  = "worker"
	RoleManager = "manager"
)

type SwarmInfo struct {
	State            string // inactive, pending, active, locked, error
	ControlAvailable bool
	ClusterID        string
	NodeID           string
	NodeAddr         string
	AdvertiseAddr    string
}

func (s SwarmInfo) Active() bool {
	return strings.EqualFold(s.State, "active")
}

func (s SwarmInfo) Manager() bool {
	return s.Active() && s.ControlAvailable
}

type SwarmNode struct {
	ID            string
	Hostname      string
	Status        string
	Availability  string
	ManagerStatus string
}

func (n SwarmNode) IsManager() bool {
	return strings.TrimSpace(n.ManagerStatus) != ""
}

func (n SwarmNode) Drained() bool {
	return strings.EqualFold(n.Availability, "drain")
}

type SwarmService struct {
	ID       string
	Name     string
	Mode     string
	Replicas string
	Image    string
}

var (
	nodeIDRe  = regexp.MustCompile(`^[a-zA-Z0-9]{8,64}$`)
	joinTokRe = regexp.MustCompile(`^SWMTKN-[0-9]+-[A-Za-z0-9._-]+$`)
	ipv4Re    = regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
)

func ParseSwarmInfo(line string) SwarmInfo {
	parts := strings.Split(strings.TrimSpace(line), "\t")
	info := SwarmInfo{State: "inactive"}
	if len(parts) > 0 && parts[0] != "" && parts[0] != "<no value>" {
		info.State = strings.ToLower(parts[0])
	}
	if len(parts) > 1 {
		info.ControlAvailable = strings.EqualFold(parts[1], "true")
	}
	if len(parts) > 2 && parts[2] != "<no value>" {
		info.ClusterID = parts[2]
	}
	if len(parts) > 3 && parts[3] != "<no value>" {
		info.NodeID = parts[3]
	}
	if len(parts) > 4 && parts[4] != "<no value>" {
		info.NodeAddr = strings.TrimSpace(parts[4])
		info.AdvertiseAddr = info.NodeAddr
	}
	return info
}

func ParseNodeLS(out string) []SwarmNode {
	var nodes []SwarmNode
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 4 {
			continue
		}
		n := SwarmNode{
			ID:           parts[0],
			Hostname:     parts[1],
			Status:       parts[2],
			Availability: parts[3],
		}
		if len(parts) > 4 {
			n.ManagerStatus = strings.TrimSpace(parts[4])
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func ParseServiceLS(out string) []SwarmService {
	var svcs []SwarmService
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		svcs = append(svcs, SwarmService{
			ID: parts[0], Name: parts[1], Mode: parts[2], Replicas: parts[3], Image: parts[4],
		})
	}
	return svcs
}

// ParseJoinCommand collapses `docker swarm join-token` help text into one line.
func ParseJoinCommand(out string) string {
	var b strings.Builder
	inCmd := false
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(line)
		if !inCmd {
			if strings.HasPrefix(trim, "docker swarm join") {
				inCmd = true
				b.WriteString(strings.TrimSuffix(trim, "\\"))
				if !strings.HasSuffix(trim, "\\") {
					break
				}
				b.WriteByte(' ')
			}
			continue
		}
		cont := strings.TrimSuffix(trim, "\\")
		b.WriteString(cont)
		if !strings.HasSuffix(trim, "\\") {
			break
		}
		b.WriteByte(' ')
	}
	cmd := strings.Join(strings.Fields(b.String()), " ")
	if !strings.HasPrefix(cmd, "docker swarm join") {
		return ""
	}
	return cmd
}

// ParseJoinParts extracts --token and host:port from a one-line join command.
func ParseJoinParts(cmd string) (token, addr string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(cmd))
	for i, f := range fields {
		if f == "--token" && i+1 < len(fields) {
			token = fields[i+1]
		}
	}
	if len(fields) > 0 {
		addr = fields[len(fields)-1]
	}
	if token == "" || addr == "" || !strings.Contains(addr, ":") {
		return "", "", false
	}
	return token, addr, true
}

func JoinListenAddr(nodeAddr, advertise string) string {
	addr := strings.TrimSpace(nodeAddr)
	if addr == "" {
		addr = strings.TrimSpace(advertise)
	}
	if addr == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "2377")
}

func NextAvailability(cur string) string {
	return nextAvailability(cur)
}

func FirstIPv4(hostnameI string) string {
	for _, tok := range strings.Fields(hostnameI) {
		ip := net.ParseIP(tok)
		if ip == nil || ip.To4() == nil || ip.IsLoopback() {
			continue
		}
		return ip.To4().String()
	}
	return ""
}

func sanitizeRole(role string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case RoleWorker, "w":
		return RoleWorker, nil
	case RoleManager, "m":
		return RoleManager, nil
	default:
		return "", fmt.Errorf("role must be worker or manager")
	}
}

func sanitizeNodeID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !nodeIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid node id")
	}
	return id, nil
}

func sanitizeToken(tok string) (string, error) {
	tok = strings.TrimSpace(tok)
	if !joinTokRe.MatchString(tok) {
		return "", fmt.Errorf("invalid join token")
	}
	return tok, nil
}

func sanitizeJoinAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = "2377"
		addr = net.JoinHostPort(host, port)
	}
	if port != "2377" {
		return "", fmt.Errorf("swarm listen port must be 2377")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return "", fmt.Errorf("invalid advertise address")
		}
		return addr, nil
	}
	if host == "" || strings.ContainsAny(host, " \t\n;|&`$") {
		return "", fmt.Errorf("invalid join address")
	}
	return addr, nil
}

func sanitizeAdvertiseIP(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		addr = h
	}
	ip := net.ParseIP(addr)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() {
		return "", fmt.Errorf("advertise address must be a non-loopback IPv4")
	}
	s := ip.To4().String()
	if !ipv4Re.MatchString(s) {
		return "", fmt.Errorf("advertise address must be a non-loopback IPv4")
	}
	return s, nil
}

func nextAvailability(cur string) string {
	switch strings.ToLower(strings.TrimSpace(cur)) {
	case "active":
		return "drain"
	case "drain":
		return "pause"
	default:
		return "active"
	}
}

func (m *Manager) SwarmInfo() SwarmInfo {
	if m.exec == nil {
		return SwarmInfo{State: "inactive"}
	}
	line := m.exec.RunQuiet(`docker info --format '{{.Swarm.LocalNodeState}}	{{.Swarm.ControlAvailable}}	{{.Swarm.Cluster.ID}}	{{.Swarm.NodeID}}	{{.Swarm.NodeAddr}}' 2>/dev/null`)
	return ParseSwarmInfo(line)
}

func (m *Manager) DetectAdvertiseAddr() string {
	if m.exec == nil {
		return ""
	}
	if ip := FirstIPv4(m.exec.RunQuiet("hostname -I 2>/dev/null")); ip != "" {
		return ip
	}
	for _, cmd := range []string{
		`curl -4 -fsS --max-time 3 https://api.ipify.org 2>/dev/null`,
		`hostname -I 2>/dev/null | awk '{print $1}'`,
	} {
		out := strings.TrimSpace(m.exec.RunQuiet(cmd))
		if ip := FirstIPv4(out); ip != "" {
			return ip
		}
		if parsed, err := sanitizeAdvertiseIP(out); err == nil {
			return parsed
		}
	}
	return ""
}

func (m *Manager) openSwarmFirewall() {
	if m.exec == nil {
		return
	}
	_ = m.exec.RunQuiet("sudo ufw allow 2377/tcp >/dev/null 2>&1 || sudo iptables -C INPUT -p tcp --dport 2377 -j ACCEPT 2>/dev/null || sudo iptables -I INPUT -p tcp --dport 2377 -j ACCEPT")
	_ = m.exec.RunQuiet("sudo ufw allow 7946/tcp >/dev/null 2>&1 || sudo iptables -C INPUT -p tcp --dport 7946 -j ACCEPT 2>/dev/null || sudo iptables -I INPUT -p tcp --dport 7946 -j ACCEPT")
	_ = m.exec.RunQuiet("sudo ufw allow 7946/udp >/dev/null 2>&1 || sudo iptables -C INPUT -p udp --dport 7946 -j ACCEPT 2>/dev/null || sudo iptables -I INPUT -p udp --dport 7946 -j ACCEPT")
	_ = m.exec.RunQuiet("sudo ufw allow 4789/udp >/dev/null 2>&1 || sudo iptables -C INPUT -p udp --dport 4789 -j ACCEPT 2>/dev/null || sudo iptables -I INPUT -p udp --dport 4789 -j ACCEPT")
}

func (m *Manager) closeSwarmFirewall() {
	if m.exec == nil {
		return
	}
	_ = m.exec.RunQuiet("sudo ufw --force delete allow 2377/tcp >/dev/null 2>&1; sudo ufw deny 2377/tcp >/dev/null 2>&1 || true")
	_ = m.exec.RunQuiet("sudo ufw --force delete allow 7946/tcp >/dev/null 2>&1; sudo ufw deny 7946/tcp >/dev/null 2>&1 || true")
	_ = m.exec.RunQuiet("sudo ufw --force delete allow 7946/udp >/dev/null 2>&1; sudo ufw deny 7946/udp >/dev/null 2>&1 || true")
	_ = m.exec.RunQuiet("sudo ufw --force delete allow 4789/udp >/dev/null 2>&1; sudo ufw deny 4789/udp >/dev/null 2>&1 || true")
	_ = m.exec.RunQuiet("sudo iptables -D INPUT -p tcp --dport 2377 -j ACCEPT 2>/dev/null; true")
	_ = m.exec.RunQuiet("sudo iptables -D INPUT -p tcp --dport 7946 -j ACCEPT 2>/dev/null; true")
	_ = m.exec.RunQuiet("sudo iptables -D INPUT -p udp --dport 7946 -j ACCEPT 2>/dev/null; true")
	_ = m.exec.RunQuiet("sudo iptables -D INPUT -p udp --dport 4789 -j ACCEPT 2>/dev/null; true")
}

func (m *Manager) InitSwarm(advertiseAddr string) error {
	addr := strings.TrimSpace(advertiseAddr)
	if addr == "" {
		addr = m.DetectAdvertiseAddr()
	}
	ip, err := sanitizeAdvertiseIP(addr)
	if err != nil {
		return err
	}
	m.openSwarmFirewall()
	_, err = m.run("docker swarm init --advertise-addr " + ip)
	return err
}

func (m *Manager) LeaveSwarm(force bool) error {
	cmd := "docker swarm leave"
	if force {
		cmd += " --force"
	}
	if _, err := m.run(cmd); err != nil {
		return err
	}
	if !m.SwarmInfo().Active() {
		m.closeSwarmFirewall()
	}
	return nil
}

func (m *Manager) JoinToken(role string) (string, error) {
	role, err := sanitizeRole(role)
	if err != nil {
		return "", err
	}
	out, err := m.run("docker swarm join-token -q " + role)
	if err != nil {
		return "", err
	}
	return sanitizeToken(strings.TrimSpace(out))
}

func (m *Manager) JoinCommand(role string) (string, error) {
	role, err := sanitizeRole(role)
	if err != nil {
		return "", err
	}
	out, err := m.run("docker swarm join-token " + role)
	if err != nil {
		return "", err
	}
	cmd := ParseJoinCommand(out)
	if cmd == "" {
		return "", fmt.Errorf("could not parse join-token output")
	}
	return cmd, nil
}

func (m *Manager) Join(token, addr string) error {
	tok, err := sanitizeToken(token)
	if err != nil {
		return err
	}
	joinAddr, err := sanitizeJoinAddr(addr)
	if err != nil {
		return err
	}
	_, err = m.run(fmt.Sprintf("docker swarm join --token %s %s", tok, joinAddr))
	return err
}

func JoinRemote(workerExec *ssh.Executor, token, managerAddr string) error {
	return NewManager(workerExec).Join(token, managerAddr)
}

func (m *Manager) ListNodes() ([]SwarmNode, error) {
	if !m.SwarmInfo().Manager() {
		return nil, nil
	}
	out, err := m.run(`docker node ls --format '{{.ID}}	{{.Hostname}}	{{.Status}}	{{.Availability}}	{{.ManagerStatus}}'`)
	if err != nil {
		return nil, err
	}
	return ParseNodeLS(out), nil
}

func (m *Manager) ListServices() ([]SwarmService, error) {
	if !m.SwarmInfo().Manager() {
		return nil, nil
	}
	out, err := m.run(`docker service ls --format '{{.ID}}	{{.Name}}	{{.Mode}}	{{.Replicas}}	{{.Image}}'`)
	if err != nil {
		return nil, err
	}
	return ParseServiceLS(out), nil
}

func (m *Manager) Promote(id string) error {
	id, err := sanitizeNodeID(id)
	if err != nil {
		return err
	}
	_, err = m.run("docker node promote " + id)
	return err
}

func (m *Manager) Demote(id string) error {
	id, err := sanitizeNodeID(id)
	if err != nil {
		return err
	}
	_, err = m.run("docker node demote " + id)
	return err
}

func (m *Manager) SetAvailability(id, avail string) error {
	id, err := sanitizeNodeID(id)
	if err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(avail)) {
	case "active", "drain", "pause":
		avail = strings.ToLower(strings.TrimSpace(avail))
	default:
		return fmt.Errorf("availability must be active, drain, or pause")
	}
	_, err = m.run("docker node update --availability " + avail + " " + id)
	return err
}

func (m *Manager) RemoveNode(id string, force bool) error {
	id, err := sanitizeNodeID(id)
	if err != nil {
		return err
	}
	cmd := "docker node rm "
	if force {
		cmd += "--force "
	}
	_, err = m.run(cmd + id)
	return err
}
