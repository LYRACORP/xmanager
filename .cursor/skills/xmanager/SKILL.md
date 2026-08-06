---
name: xmanager
description: >-
  XManager TUI + optional web/MCP (lyracorp/xmanager): Go + Bubble Tea SSH VPS/PaaS orchestrator.
  Use for any work in this repo — architecture, bugs, features, refactors.
---

# XManager — project context

## Product constraints

- **SSH-only, zero server agent** — all remote ops via `internal/ssh`; no daemons on managed hosts.
- **TUI primary** — Bubble Tea is the default UI; optional HTMX web panel is off by default.
- **Optional services** — registry/gitea/k8s/etc. are opt-in per server (disabled by default).

## Module & build

- Import path: `github.com/lyracorp/xmanager`
- Entry: `cmd/xmanager/main.go` → TUI / `web` / `mcp`
- Build: `make build`
- Verify: `make test`, `make lint`

## Package map

| Path | Role |
|------|------|
| `internal/tui/` | FleetOverview home + screens |
| `internal/web/` | HTMX web panel |
| `internal/mcp/` | MCP stdio tools for AI agents |
| `internal/poller/` | SSH metrics + uptime polling |
| `internal/ssh/` | Client, pool, executor, SFTP |
| `internal/project/` | Deploy engine (10 project types) |
| `internal/services/` | Optional self-hosted stacks |
| `internal/ai/` | Providers + agent helpers |
| `apps/` | One-click CapRover YAML apps |

## Related skills

| Task | Skill |
|------|--------|
| New/changed TUI screen | `xmanager-tui-screen` |
| Remote commands / SSH | `xmanager-ssh` |
| AI provider | `xmanager-ai` |
| Wizard YAML | `xmanager-wizard` |
| Commits, PRs, secrets | `xmanager-public-contrib` |
