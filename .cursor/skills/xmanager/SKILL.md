---
name: xmanager
description: >-
  XManager TUI (lyracorp/xmanager): Go + Bubble Tea SSH VPS orchestrator.
  Use for any work in this repo — architecture, bugs, features, refactors.
---

# XManager — project context

## Product constraints (do not violate)

- **SSH-only, zero server agent** — all remote ops via `internal/ssh`; never add daemons/installers on managed hosts.
- **Terminal-only** — no web UI.
- **Out of scope**: Kubernetes, server-side agents, telemetry.

## Module & build

- Import path: `github.com/lyracorp/xmanager`
- Entry: `cmd/xmanager/main.go` → `internal/tui`
- Build: `make build` (requires **CGO_ENABLED=1** for SQLite)
- Verify: `make test`, `make lint`

## Package map (read on demand)

| Path | Role |
|------|------|
| `internal/tui/` | App, router, screens, components, theme |
| `internal/ssh/` | Client, pool, executor, SFTP |
| `internal/ai/` | Provider interface + OpenAI/Anthropic/Ollama |
| `internal/storage/` | GORM + SQLite models/migrations |
| `internal/config/` | Viper + AES-256-GCM for secrets |
| `internal/docker`, `pm2`, `proxy`, `dbmanager`, `backup`, `recon`, `errtrack`, `notify` | Domain logic over SSH |
| `wizards/*.yaml` | Setup wizard step definitions |

## Token-efficient exploration

1. **Grep first** — locate symbols before opening large `model.go` files.
2. **One reference screen** — for TUI work, read `internal/tui/screens/dashboard/model.go` (async cmds + parsing) unless editing another screen.
3. **Skip** `go.sum`, generated assets, and unrelated `internal/*` packages.
4. **Minimal diff** — match existing screen/package layout; no drive-by refactors.

## Code style (from CONTRIBUTING)

- `gofmt` / `goimports`; self-documenting code, sparse comments
- Errors: lowercase, no trailing period; wrap with `fmt.Errorf("context: %w", err)`

## Related skills (load only when needed)

| Task | Skill |
|------|--------|
| New/changed TUI screen | `xmanager-tui-screen` |
| Remote commands / SSH | `xmanager-ssh` |
| AI provider | `xmanager-ai` |
| Wizard YAML | `xmanager-wizard` |
| Commits, PRs, secrets | `xmanager-public-contrib` |

## Public repo

Never commit real API keys, bot tokens, passwords, or private hostnames. See `xmanager-public-contrib`.
