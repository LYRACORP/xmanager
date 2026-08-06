package ai

import (
	"context"
	"fmt"
	"strings"
)

// Agent wraps a Provider with a persistent conversation history and a
// server-management system prompt.
type Agent struct {
	provider Provider
	history  []Message
}

// NewAgent creates an Agent backed by the given Provider.
// The conversation is pre-seeded with BuildAgentSystemPrompt.
func NewAgent(p Provider) *Agent {
	return &Agent{
		provider: p,
		history: []Message{
			{Role: RoleSystem, Content: BuildAgentSystemPrompt()},
		},
	}
}

// Send appends userMsg to the conversation, requests a completion, records
// the assistant reply, and returns it.
func (a *Agent) Send(ctx context.Context, userMsg string, opts ...Option) (string, error) {
	a.history = append(a.history, Message{Role: RoleUser, Content: userMsg})

	reply, err := a.provider.Chat(ctx, a.history, opts...)
	if err != nil {
		// remove the unmatched user message on error
		a.history = a.history[:len(a.history)-1]
		return "", fmt.Errorf("agent chat: %w", err)
	}

	a.history = append(a.history, Message{Role: RoleAssistant, Content: reply})
	return reply, nil
}

// Reset clears conversation history, keeping only the system prompt.
func (a *Agent) Reset() {
	a.history = []Message{
		{Role: RoleSystem, Content: BuildAgentSystemPrompt()},
	}
}

// History returns a snapshot of the current conversation.
func (a *Agent) History() []Message {
	out := make([]Message, len(a.history))
	copy(out, a.history)
	return out
}

// Provider returns the underlying AI provider.
func (a *Agent) Provider() Provider { return a.provider }

// BuildAgentSystemPrompt returns a system prompt that describes all XManager
// MCP tools and operational guidelines for the agent.
func BuildAgentSystemPrompt() string {
	var b strings.Builder

	b.WriteString("You are XManager AI, an expert VPS orchestration assistant integrated\n")
	b.WriteString("with the XManager platform (github.com/lyracorp/xmanager).\n\n")

	b.WriteString("## Platform overview\n")
	b.WriteString("XManager manages Linux servers exclusively over SSH — no daemons are\n")
	b.WriteString("installed on managed hosts. It uses GORM/SQLite for local state and\n")
	b.WriteString("Bubble Tea for the TUI.\n\n")

	b.WriteString("## Available MCP tools\n\n")

	type toolEntry struct{ name, sig, desc string }
	tools := []toolEntry{
		{
			"list_servers",
			"list_servers()",
			"Return all servers in the database with their connection metadata and last-seen time.",
		},
		{
			"get_server_metrics",
			"get_server_metrics(server_id: int)",
			"Return the most recent CPU %, RAM %, disk %, and network throughput snapshot for the server.",
		},
		{
			"list_containers",
			"list_containers(server_id: int)",
			"Run `docker ps -a` over SSH and return id, name, image, state, ports for every container.",
		},
		{
			"deploy_project",
			"deploy_project(project_id: int)",
			"Deploy a project using its configured strategy (image, compose, git, dockerfile, …).",
		},
		{
			"run_script",
			"run_script(name, script_type, content, server_ids?, all_servers?)",
			"Execute a bash/python/node script on one or more servers concurrently.",
		},
		{
			"list_cron_jobs",
			"list_cron_jobs(server_id: int)",
			"List all cron jobs stored in the DB for the server.",
		},
		{
			"manage_cron",
			"manage_cron(action, server_id, job_id?, name?, expression?, command?)",
			"Add, remove, enable, or disable a cron job. action must be one of: add, remove, enable, disable.",
		},
		{
			"get_uptime",
			"get_uptime(server_id?: int)",
			"Return uptime monitors with last_status (up/down/degraded). server_id=0 returns all.",
		},
		{
			"list_projects",
			"list_projects(server_id?: int)",
			"Return projects, optionally scoped to one server.",
		},
		{
			"server_recon",
			"server_recon(server_id: int)",
			"Run a port scan via `ss -tlnp` over SSH. Returns open TCP ports and process names.",
		},
		{
			"list_services",
			"list_services(server_id: int)",
			"List ServiceInstance records (gitea, kafka, mattermost, rabbitmq, registry, rustfs).",
		},
		{
			"enable_service",
			"enable_service(server_id: int, service_type: string)",
			"Enable a service: writes DB record and runs docker-compose up via SSH if connected.",
		},
		{
			"disable_service",
			"disable_service(server_id: int, service_type: string)",
			"Disable a service: runs docker-compose down via SSH and updates DB record.",
		},
	}

	for _, t := range tools {
		b.WriteString(fmt.Sprintf("### %s\n`%s`\n%s\n\n", t.name, t.sig, t.desc))
	}

	b.WriteString("## Guidelines\n\n")
	b.WriteString("- **Confirm before destructive actions**: always ask before deploy_project,\n")
	b.WriteString("  disable_service, manage_cron(remove), or run_script on production servers.\n")
	b.WriteString("- **SSH prerequisite**: list_containers, deploy_project, run_script, manage_cron,\n")
	b.WriteString("  server_recon, enable_service, and disable_service require the server to be\n")
	b.WriteString("  actively connected in the SSH pool.\n")
	b.WriteString("- **Present data clearly**: use tables or structured lists when returning\n")
	b.WriteString("  multi-row results (servers, containers, projects, etc.).\n")
	b.WriteString("- **Error guidance**: when a tool returns an error, explain the likely cause\n")
	b.WriteString("  and suggest a remediation step.\n")
	b.WriteString("- **Minimal scope**: prefer targeted server IDs over all_servers unless the\n")
	b.WriteString("  user explicitly requests a fleet-wide operation.\n")

	return b.String()
}
