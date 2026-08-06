package recon

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
)

// OpenPort describes a listening TCP port on a remote server.
type OpenPort struct {
	Port     int
	Protocol string // tcp, tcp6
	Process  string // process name from ss -p, if available
	Addr     string // listening address
}

// PingResult holds the result of a TCP-level reachability check.
type PingResult struct {
	Host      string
	Port      int
	Reachable bool
	LatencyMS int64
}

// ScanPorts uses `ss -tlnp` over SSH to list listening TCP ports.
func ScanPorts(exec *ssh.Executor) ([]OpenPort, error) {
	result, err := exec.Run("ss -tlnp 2>/dev/null || netstat -tlnp 2>/dev/null")
	if err != nil {
		return nil, fmt.Errorf("port scan: %w", err)
	}
	return parseSS(result.Stdout), nil
}

func parseSS(out string) []OpenPort {
	var ports []OpenPort
	seen := make(map[int]bool)

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "State") || strings.HasPrefix(line, "Netid") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		// ss output: State Recv-Q Send-Q Local-Address:Port Peer ...  process
		// netstat:   tcp  0  0  0.0.0.0:80  0.0.0.0:*  LISTEN  1234/nginx
		var addrPort string
		proto := strings.ToLower(fields[0])
		if proto == "tcp" || proto == "tcp6" || proto == "listen" {
			if len(fields) >= 4 {
				addrPort = fields[3]
			}
		} else if strings.HasPrefix(proto, "tcp") {
			addrPort = fields[3]
		} else {
			continue
		}

		addr, portStr := splitAddrPort(addrPort)
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || seen[port] {
			continue
		}
		seen[port] = true

		process := ""
		for _, f := range fields[5:] {
			if strings.Contains(f, `"`) {
				start := strings.Index(f, `"`)
				end := strings.LastIndex(f, `"`)
				if end > start {
					process = f[start+1 : end]
				}
				break
			}
		}

		ports = append(ports, OpenPort{
			Port:     port,
			Protocol: proto,
			Addr:     addr,
			Process:  process,
		})
	}
	return ports
}

// splitAddrPort handles IPv4, IPv6 bracket notation, and plain port.
func splitAddrPort(s string) (addr, port string) {
	if strings.HasPrefix(s, "[") {
		end := strings.LastIndex(s, "]")
		if end < 0 {
			return s, ""
		}
		addr = s[1:end]
		rest := s[end+1:]
		if strings.HasPrefix(rest, ":") {
			port = rest[1:]
		}
		return
	}
	last := strings.LastIndex(s, ":")
	if last < 0 {
		return s, ""
	}
	return s[:last], s[last+1:]
}

// PingPort dials the remote host:port from the local machine to verify reachability.
func PingPort(host string, port int, timeout time.Duration) PingResult {
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, timeout)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return PingResult{Host: host, Port: port, Reachable: false, LatencyMS: latency}
	}
	conn.Close()
	return PingResult{Host: host, Port: port, Reachable: true, LatencyMS: latency}
}

// PingHost checks reachability of port 22 (or any provided port) as a proxy for host liveness.
func PingHost(host string, timeout time.Duration) PingResult {
	return PingPort(host, 22, timeout)
}

// ScanAndPing combines ScanPorts (via SSH) with local TCP dials to annotate which
// ports are actually reachable from the controlling machine.
func ScanAndPing(exec *ssh.Executor, host string, timeout time.Duration) ([]OpenPort, []PingResult, error) {
	ports, err := ScanPorts(exec)
	if err != nil {
		return nil, nil, err
	}

	results := make([]PingResult, len(ports))
	for i, p := range ports {
		results[i] = PingPort(host, p.Port, timeout)
	}
	return ports, results, nil
}
