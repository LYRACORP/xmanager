# XManager

**AI-powered TUI + optional web panel for VPS / PaaS orchestration**

> Manage a fleet of servers over SSH — deploy projects, monitor metrics, run scripts, and toggle self-hosted services. Zero agents on managed hosts.

[![CI](https://github.com/lyracorp/xmanager/actions/workflows/ci.yml/badge.svg)](https://github.com/lyracorp/xmanager/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/lyracorp/xmanager)](go.mod)

## Highlights

- **Fleet Overview** — all servers on one home screen (TUI + web) with live CPU / RAM / disk / network / container metrics
- **SSH-only** — no daemons installed on managed servers; optional pure SSH polling
- **Optional HTMX web panel** — laptop **control** fleet UI (`xmanager web`); per-server **node** panel via `w` (that host’s metrics only)
- **PaaS projects** — image, compose, Dockerfile, git, one-click apps, archive, functions, and more
- **Scripts & cron** — run bash/python/node on one / many / all servers; manage remote cron jobs
- **Databases** — MySQL, MariaDB, PostgreSQL, MongoDB, ClickHouse, Redis + backups
- **Optional services** — Docker Registry, Gitea, RustFS, RabbitMQ, Kafka, Mattermost, GlitchTip, Netdata (+auth), Umami, PowerDNS, mail, Kubernetes (kubespray)
- **Uptime monitoring** — HTTP/TCP checks with Telegram / email / webhook / SMS alerts
- **MCP server** — `xmanager mcp` exposes management tools for AI agents
- **AI chat / recon** — OpenAI, Anthropic, or Ollama

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash
# or
make build && sudo make install
```

## Quick start

```bash
xmanager              # Fleet Overview TUI (default)
xmanager setup        # First-time wizard
xmanager connect name # Jump to a server
xmanager web          # HTMX web panel (auth on first visit)
xmanager mcp          # MCP stdio server for AI agents
```

Keyboard (global): `Ctrl+F` fleet · `Ctrl+A` AI chat · `?` help · `Esc` back

Fleet Overview: `Enter` connect · `w` install **node** web panel (or reinstall/upgrade / uninstall if already present) · `a` add · `d` delete

From a server dashboard: `d` Docker · `p` PM2 · `l` logs · `j` projects · `o` cron · `t` scripts · `y` uptime · `v` services · `z` recon · `n` database · `w` node web panel (same install / upgrade / uninstall flow)

### Control web vs node web panel

| | **Control** (`xmanager web` on your laptop) | **Node** (`w` install on a managed server) |
|--|---------------------------------------------|--------------------------------------------|
| Role | Multi-server fleet UI | Dashboard for **that server only** |
| Config | `web.role: control` (default) | `web.role: node` (written on install) |
| Home page | All servers / projects | CPU, RAM, disk, net, containers, ports |
| URL | e.g. `http://127.0.0.1:8080` | e.g. `http://<server-ip>:8080` |

### Fleet Overview vs server Dashboard

| | **Fleet Overview** (home) | **Dashboard** (per server) |
|--|---------------------------|----------------------------|
| What | All servers on one screen | One connected server in depth |
| Metrics | CPU/RAM/disk cards for every host | Live gauges, processes, files, services for that host |
| How to open | Start of app, or `Ctrl+F` | `Enter` on a fleet card |
| Typical use | Pick a server, see who’s up, install web panel (`w`) | Manage Docker/PM2/logs/projects on that box |

## Configuration

`~/.config/xmanager/config.yaml`:

```yaml
ai:
  provider: ollama          # openai | anthropic | ollama
  model: llama3
  api_key: ""
  ollama_host: http://localhost:11434

telegram:
  enabled: false
  bot_token: ""
  chat_id: ""

web:
  enabled: false            # optional panel; also: xmanager web
  host: 127.0.0.1
  port: 8080

poller:
  interval_sec: 30
  metric_retention: 288     # ~24h at 5min samples
  uptime_interval_sec: 60

ui:
  theme: dark
  refresh_rate: 5
```

## Architecture

```
┌──────────────────────────────────────────────┐
│     TUI (Bubble Tea)  │  Web (HTMX)  │  MCP  │
├──────────────────────────────────────────────┤
│  Core: projects, scripts, cron, db, services │
├──────────────────────────────────────────────┤
│  Poller (metrics + uptime) + Notify channels │
├──────────────────────────────────────────────┤
│  SSH pool / executor / SFTP  → remote hosts  │
└──────────────────────────────────────────────┘
```

## Project layout

```
cmd/xmanager/          CLI entry (tui, web, mcp, setup, …)
internal/
  tui/                 FleetOverview + domain screens
  web/                 HTMX panel (templates embedded)
  mcp/                 MCP JSON-RPC stdio tools
  poller/              SSH metric + uptime polling
  project/             Deploy engine (10 project types)
  scripts/ cron/ recon/ uptime/
  services/            Optional self-hosted stacks
  docker/ dbmanager/ backup/ proxy/ …
apps/                  One-click CapRover-compatible YAMLs
wizards/               Setup wizard definitions
```

## License

MIT — see [LICENSE](LICENSE).
