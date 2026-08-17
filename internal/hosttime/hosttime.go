package hosttime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lyracorp/xmanager/internal/ssh"
)

var zoneName = regexp.MustCompile(`^[A-Za-z0-9/_+-]+$`)

const timeLayout = "2006-01-02 15:04:05"

// Status is the host clock as reported by timedatectl + date.
type Status struct {
	Timezone        string
	LocalTime       string
	NTP             bool
	NTPSynchronized bool
	LocalRTC        bool
	CanNTP          bool
}

// ClockLocalValue is an HTML datetime-local value (YYYY-MM-DDTHH:MM).
func (st Status) ClockLocalValue() string {
	parts := strings.Fields(st.LocalTime)
	if len(parts) < 2 {
		return ""
	}
	t := parts[1]
	if len(t) >= 5 {
		t = t[:5]
	}
	return parts[0] + "T" + t
}

// CmdSyncNTP is the NTP step fallback chain (chrony / ntpdate / timedatectl).
func CmdSyncNTP() string {
	return `chronyc makestep 2>/dev/null || ntpdate -u pool.ntp.org 2>/dev/null || timedatectl timesync-status 2>/dev/null || date`
}

// SanitizeTimezone validates an IANA zone name.
func SanitizeTimezone(zone string) (string, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" {
		return "", fmt.Errorf("timezone required")
	}
	if strings.ContainsAny(zone, " \t\n;|&`$()<>") || !zoneName.MatchString(zone) {
		return "", fmt.Errorf("invalid timezone")
	}
	if strings.Contains(zone, "..") {
		return "", fmt.Errorf("invalid timezone")
	}
	return zone, nil
}

// ParseClock accepts "2006-01-02 15:04:05" or HTML datetime-local "2006-01-02T15:04[:05]".
func ParseClock(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "T", " ")
	if s == "" {
		return "", fmt.Errorf("time required")
	}
	for _, layout := range []string{timeLayout, "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Format(timeLayout), nil
		}
	}
	return "", fmt.Errorf("time must be YYYY-MM-DD HH:MM[:SS]")
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}

// ParseShow parses `timedatectl show` key=value output.
func ParseShow(out string) Status {
	st := Status{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "Timezone":
			st.Timezone = strings.TrimSpace(v)
		case "NTP":
			st.NTP = parseBool(v)
		case "NTPSynchronized":
			st.NTPSynchronized = parseBool(v)
		case "LocalRTC":
			st.LocalRTC = parseBool(v)
		case "CanNTP":
			st.CanNTP = parseBool(v)
		}
	}
	return st
}

// ParseTimezones splits `timedatectl list-timezones` output.
func ParseTimezones(out string) []string {
	var zones []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if z, err := SanitizeTimezone(line); err == nil {
			zones = append(zones, z)
		}
	}
	return zones
}

func combinedErr(res *ssh.ExecResult, err error) error {
	if err != nil && (res == nil || res.ExitCode != 0) {
		msg := err.Error()
		if res != nil && res.Stderr != "" {
			msg = res.Stderr
		}
		return fmt.Errorf("%s", strings.TrimSpace(msg))
	}
	if res != nil && res.ExitCode != 0 {
		msg := res.Stderr
		if msg == "" {
			msg = res.Stdout
		}
		if msg == "" {
			msg = "exit " + strconv.Itoa(res.ExitCode)
		}
		return fmt.Errorf("%s", strings.TrimSpace(msg))
	}
	return nil
}

// Status reads timedatectl show and the local date string.
func ReadStatus(exec *ssh.Executor) (Status, error) {
	if exec == nil {
		return Status{}, fmt.Errorf("not connected")
	}
	res, err := exec.Run("timedatectl show")
	if err2 := combinedErr(res, err); err2 != nil {
		return Status{}, fmt.Errorf("timedatectl: %w", err2)
	}
	st := ParseShow(res.Stdout)
	st.LocalTime = strings.TrimSpace(exec.RunQuiet(`date '+%Y-%m-%d %H:%M:%S %z'`))
	return st, nil
}

// ListTimezones returns IANA zones from timedatectl.
func ListTimezones(exec *ssh.Executor) ([]string, error) {
	if exec == nil {
		return nil, fmt.Errorf("not connected")
	}
	res, err := exec.Run("timedatectl list-timezones")
	if err2 := combinedErr(res, err); err2 != nil {
		return nil, fmt.Errorf("list-timezones: %w", err2)
	}
	return ParseTimezones(res.Stdout), nil
}

// SetTimezone applies an IANA timezone.
func SetTimezone(exec *ssh.Executor, zone string) error {
	zone, err := SanitizeTimezone(zone)
	if err != nil {
		return err
	}
	if exec == nil {
		return fmt.Errorf("not connected")
	}
	res, err := exec.Run("timedatectl set-timezone " + zone)
	if err2 := combinedErr(res, err); err2 != nil {
		return fmt.Errorf("set-timezone: %w", err2)
	}
	return nil
}

// SetTime sets the hardware/system clock (fails if NTP is on).
func SetTime(exec *ssh.Executor, clock string) error {
	formatted, err := ParseClock(clock)
	if err != nil {
		return err
	}
	if exec == nil {
		return fmt.Errorf("not connected")
	}
	res, err := exec.Run("timedatectl set-time '" + formatted + "'")
	if err2 := combinedErr(res, err); err2 != nil {
		return fmt.Errorf("set-time: %w", err2)
	}
	return nil
}

// SetNTP enables or disables NTP.
func SetNTP(exec *ssh.Executor, on bool) error {
	if exec == nil {
		return fmt.Errorf("not connected")
	}
	v := "false"
	if on {
		v = "true"
	}
	res, err := exec.Run("timedatectl set-ntp " + v)
	if err2 := combinedErr(res, err); err2 != nil {
		return fmt.Errorf("set-ntp: %w", err2)
	}
	return nil
}

// SyncNTP steps the clock via chrony/ntpdate/timedatectl.
func SyncNTP(exec *ssh.Executor) (string, error) {
	if exec == nil {
		return "", fmt.Errorf("not connected")
	}
	res, err := exec.Run(CmdSyncNTP())
	out := ""
	if res != nil {
		out = strings.TrimSpace(res.Stdout + "\n" + res.Stderr)
	}
	if err2 := combinedErr(res, err); err2 != nil {
		return out, fmt.Errorf("sync ntp: %w", err2)
	}
	return out, nil
}
