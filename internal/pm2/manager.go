package pm2

import (
	"encoding/json"
	"fmt"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type Process struct {
	PMID      int     `json:"pm_id"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	Memory    int64   `json:"memory"`
	Restarts  int     `json:"restarts"`
	Uptime    int64   `json:"uptime"`
}

type Manager struct {
	exec *ssh.Executor
}

func NewManager(exec *ssh.Executor) *Manager {
	return &Manager{exec: exec}
}

func (m *Manager) IsAvailable() bool {
	return m.exec.RunQuiet("which pm2") != ""
}

func (m *Manager) List() ([]Process, error) {
	result, err := m.exec.Run("pm2 jlist 2>/dev/null")
	if err != nil {
		return nil, err
	}
	if result.Stdout == "" {
		return nil, nil
	}

	var raw []struct {
		PMID  int    `json:"pm_id"`
		Name  string `json:"name"`
		Monit struct {
			CPU    float64 `json:"cpu"`
			Memory int64   `json:"memory"`
		} `json:"monit"`
		PM2Env struct {
			Status      string `json:"status"`
			RestartTime int    `json:"restart_time"`
			PMUptime    int64  `json:"pm_uptime"`
		} `json:"pm2_env"`
	}

	if err := json.Unmarshal([]byte(result.Stdout), &raw); err != nil {
		return nil, fmt.Errorf("parsing pm2 output: %w", err)
	}

	procs := make([]Process, len(raw))
	for i, r := range raw {
		procs[i] = Process{
			PMID:     r.PMID,
			Name:     r.Name,
			Status:   r.PM2Env.Status,
			CPU:      r.Monit.CPU,
			Memory:   r.Monit.Memory,
			Restarts: r.PM2Env.RestartTime,
			Uptime:   r.PM2Env.PMUptime,
		}
	}
	return procs, nil
}

func (m *Manager) Restart(id int) error {
	_, err := m.exec.Run(fmt.Sprintf("pm2 restart %d", id))
	return err
}

func (m *Manager) Stop(id int) error {
	_, err := m.exec.Run(fmt.Sprintf("pm2 stop %d", id))
	return err
}

func (m *Manager) Delete(id int) error {
	_, err := m.exec.Run(fmt.Sprintf("pm2 delete %d", id))
	return err
}

func (m *Manager) FlushLogs(id int) error {
	_, err := m.exec.Run(fmt.Sprintf("pm2 flush %d", id))
	return err
}

func (m *Manager) Logs(id int, lines int) (string, error) {
	result, err := m.exec.Run(fmt.Sprintf("pm2 logs %d --lines %d --nostream 2>&1", id, lines))
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}
