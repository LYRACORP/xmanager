package localnet

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Counters is a point-in-time interface byte counter snapshot.
type Counters struct {
	Iface string
	Rx    uint64
	Tx    uint64
	At    time.Time
}

// Sample returns counters for the busiest non-loopback interface.
func Sample() (Counters, error) {
	switch runtime.GOOS {
	case "linux":
		return sampleLinux()
	case "darwin":
		return sampleDarwin()
	default:
		return Counters{At: time.Now()}, fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
}

// Rates computes receive/transmit bytes-per-second between two samples.
func Rates(prev, cur Counters) (rxBps, txBps float64) {
	dt := cur.At.Sub(prev.At).Seconds()
	if dt <= 0 {
		return 0, 0
	}
	var dRx, dTx float64
	if cur.Rx >= prev.Rx {
		dRx = float64(cur.Rx - prev.Rx)
	}
	if cur.Tx >= prev.Tx {
		dTx = float64(cur.Tx - prev.Tx)
	}
	return dRx / dt, dTx / dt
}

// FormatRate formats a bytes/sec rate for display.
func FormatRate(bps float64) string {
	if bps < 1024 {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	if bps < 1024*1024 {
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	}
	return fmt.Sprintf("%.2f MB/s", bps/(1024*1024))
}

// FormatBytes formats a byte count.
func FormatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func sampleLinux() (Counters, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return Counters{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var best Counters
	lineNo := 0
	for sc.Scan() {
		lineNo++
		if lineNo <= 2 {
			continue
		}
		line := strings.TrimSpace(sc.Text())
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "lo" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		if rx+tx >= best.Rx+best.Tx {
			best = Counters{Iface: name, Rx: rx, Tx: tx}
		}
	}
	best.At = time.Now()
	if best.Iface == "" {
		return best, fmt.Errorf("no network interface")
	}
	return best, nil
}

func sampleDarwin() (Counters, error) {
	out, err := exec.Command("netstat", "-ib").Output()
	if err != nil {
		return Counters{}, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	var best Counters
	header := true
	ibytesIdx, obytesIdx, nameIdx := -1, -1, 0
	seen := map[string]Counters{}
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if header {
			header = false
			for i, f := range fields {
				switch f {
				case "Name":
					nameIdx = i
				case "Ibytes":
					ibytesIdx = i
				case "Obytes":
					obytesIdx = i
				}
			}
			if ibytesIdx < 0 || obytesIdx < 0 {
				return Counters{}, fmt.Errorf("unexpected netstat -ib header")
			}
			continue
		}
		if len(fields) <= obytesIdx || len(fields) <= ibytesIdx {
			continue
		}
		name := fields[nameIdx]
		if name == "lo0" || strings.HasPrefix(name, "awdl") || strings.HasPrefix(name, "llw") || strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "bridge") {
			continue
		}
		rx, _ := strconv.ParseUint(fields[ibytesIdx], 10, 64)
		tx, _ := strconv.ParseUint(fields[obytesIdx], 10, 64)
		// netstat -ib repeats Name per address family; keep max counters per iface.
		prev := seen[name]
		if rx > prev.Rx {
			prev.Rx = rx
		}
		if tx > prev.Tx {
			prev.Tx = tx
		}
		prev.Iface = name
		seen[name] = prev
	}
	for _, c := range seen {
		if c.Rx+c.Tx >= best.Rx+best.Tx {
			best = c
		}
	}
	best.At = time.Now()
	if best.Iface == "" {
		return best, fmt.Errorf("no network interface")
	}
	return best, nil
}
