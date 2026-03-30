package ai

import (
	"fmt"
	"strings"

	"github.com/lyracorp/xmanager/internal/ssh"
)

type ServerContext struct {
	ServerName    string
	Profile       string
	ResourceUsage string
	RecentLogs    string
	ActiveAlerts  string
}

func BuildSystemPrompt(sctx ServerContext) Message {
	var sb strings.Builder
	sb.WriteString("You are XManager AI, an expert DevOps assistant. ")
	sb.WriteString("You help manage Linux servers via SSH. You have deep knowledge of Docker, Nginx, PostgreSQL, MySQL, MongoDB, PM2, systemd, and general Linux administration.\n\n")

	if sctx.ServerName != "" {
		sb.WriteString(fmt.Sprintf("## Current Server: %s\n\n", sctx.ServerName))
	}

	if sctx.Profile != "" {
		sb.WriteString("## Server Profile\n")
		sb.WriteString(sctx.Profile)
		sb.WriteString("\n\n")
	}

	if sctx.ResourceUsage != "" {
		sb.WriteString("## Resource Usage\n")
		sb.WriteString(sctx.ResourceUsage)
		sb.WriteString("\n\n")
	}

	if sctx.RecentLogs != "" {
		sb.WriteString("## Recent Logs\n```\n")
		sb.WriteString(sctx.RecentLogs)
		sb.WriteString("\n```\n\n")
	}

	if sctx.ActiveAlerts != "" {
		sb.WriteString("## Active Alerts\n")
		sb.WriteString(sctx.ActiveAlerts)
		sb.WriteString("\n\n")
	}

	sb.WriteString("When suggesting commands, always show the exact command to run. ")
	sb.WriteString("Warn about destructive operations. Be concise but thorough.")

	return Message{Role: RoleSystem, Content: sb.String()}
}

func GatherResourceUsage(exec *ssh.Executor) string {
	var sb strings.Builder

	if cpu := exec.RunQuiet("top -bn1 | grep 'Cpu(s)' | head -1"); cpu != "" {
		sb.WriteString("CPU: " + cpu + "\n")
	}
	if mem := exec.RunQuiet("free -h | head -3"); mem != "" {
		sb.WriteString("Memory:\n" + mem + "\n")
	}
	if disk := exec.RunQuiet("df -h / | tail -1"); disk != "" {
		sb.WriteString("Disk: " + disk + "\n")
	}
	if load := exec.RunQuiet("uptime"); load != "" {
		sb.WriteString("Load: " + load + "\n")
	}

	return sb.String()
}

func GatherRecentLogs(exec *ssh.Executor, service string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 200
	}

	var cmd string
	switch {
	case strings.HasPrefix(service, "docker:"):
		container := strings.TrimPrefix(service, "docker:")
		cmd = fmt.Sprintf("docker logs --tail %d %s 2>&1", maxLines, container)
	case strings.HasPrefix(service, "pm2:"):
		name := strings.TrimPrefix(service, "pm2:")
		cmd = fmt.Sprintf("pm2 logs %s --lines %d --nostream 2>&1", name, maxLines)
	case strings.HasPrefix(service, "journal:"):
		unit := strings.TrimPrefix(service, "journal:")
		cmd = fmt.Sprintf("journalctl -u %s -n %d --no-pager 2>&1", unit, maxLines)
	default:
		cmd = fmt.Sprintf("journalctl -n %d --no-pager 2>&1", maxLines)
	}

	return exec.RunQuiet(cmd)
}
