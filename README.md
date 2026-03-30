# XManager TUI

**AI-Powered Terminal UI for VPS Orchestration**

> Manage any server like a senior DevOps engineer — from your terminal.

[![CI](https://github.com/lyracorp/xmanager/actions/workflows/ci.yml/badge.svg)](https://github.com/lyracorp/xmanager/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/lyracorp/xmanager)](go.mod)

XManager TUI is a lightweight, AI-powered terminal application that connects to remote servers via SSH and provides a rich keyboard-driven interface for complete server lifecycle management. **Zero server-side footprint** — no agents, no daemons, no ports opened.

## Features

- **AI Server Reconnaissance** — Connect to any server, AI maps the full stack in seconds
- **Docker Management** — Containers, Compose stacks, images, registry — all via SSH
- **PM2 / Process Management** — View, start, stop, restart PM2 and systemd services
- **Real-time Log Viewer** — Multi-source streaming with search, filter, and AI analysis
- **AI Chat Assistant** — Context-aware Q&A with full server knowledge
- **Setup Wizard** — Raw VPS to production-ready server via AI-guided steps
- **Error Tracking** — Sentry-like error capture, grouping, and Telegram alerts
- **Database Manager** — PostgreSQL, MySQL, MongoDB — users, DBs, backups
- **Nginx / Traefik Manager** — Visual vhost, SSL, routing management
- **Backup Manager** — Scheduled DB dumps and volume snapshots
- **Multi-Server Dashboard** — Overview all servers from one screen
- **Telegram Notifications** — Push alerts for errors, deploys, and resource thresholds

## What XManager Does NOT Do

- No Kubernetes support — if you need K8s, you've outgrown this tool
- No web UI — terminal-first is a feature, not a limitation
- No server-side agents — pure SSH, zero footprint
- No telemetry or data collection

## Installation

### Quick Install (Linux/macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/lyracorp/xmanager/main/install.sh | bash
```

### Homebrew (macOS)

```bash
brew install lyracorp/tap/xmanager
```

### Build from Source

```bash
git clone https://github.com/lyracorp/xmanager.git
cd xmanager
make build
sudo make install
```

### Docker

```bash
docker run --rm -it -v ~/.ssh:/root/.ssh:ro -v ~/.config/xmanager:/root/.config/xmanager ghcr.io/lyracorp/xmanager
```

## Quick Start

### 1. First Run

```bash
xmanager
# or use the alias:
vpsm
```

The first-run setup wizard will guide you through:
1. Configuring your AI provider (OpenAI / Claude / Ollama)
2. Adding your first server
3. Running AI reconnaissance

### 2. Add a Server

From the Server List screen, press `a` to add a server:

| Field | Example |
|-------|---------|
| Name | my-production |
| Host | 192.168.1.100 |
| Port | 22 |
| User | root |
| SSH Key | ~/.ssh/id_rsa |
| Tags | web,prod |

### 3. Connect and Explore

Press `Enter` on a server to connect. XManager will:
- Establish an SSH connection
- Show the Server Dashboard with resource gauges
- Let you navigate to Docker, PM2, Logs, and more

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Navigate between panels |
| `j` / `k` or `Up` / `Down` | Move within lists |
| `Enter` | Select / confirm |
| `Esc` / `q` | Go back |
| `?` | Show help |
| `/` | Search / filter |
| `Ctrl+A` | Open AI chat |
| `Ctrl+S` | Switch server |
| `Ctrl+R` | Refresh view |

## AI Provider Setup

XManager supports three AI providers:

### OpenAI (Default)

```yaml
# ~/.config/xmanager/config.yaml
ai:
  provider: openai
  model: gpt-4o
  api_key: sk-...
```

### Anthropic Claude

```yaml
ai:
  provider: anthropic
  model: claude-sonnet-4-20250514
  api_key: sk-ant-...
```

### Ollama (Local / Offline)

```yaml
ai:
  provider: ollama
  model: llama3
  ollama_host: http://localhost:11434
```

No API key needed. Full privacy — nothing leaves your machine.

## Configuration

XManager stores config at `~/.config/xmanager/config.yaml`:

```yaml
ai:
  provider: ollama
  model: llama3
  api_key: ""
  ollama_host: http://localhost:11434
  max_log_lines: 200

telegram:
  bot_token: ""
  chat_id: ""
  enabled: false

ui:
  theme: dark        # dark | light
  refresh_rate: 5    # seconds
```

Environment variable overrides: `XMANAGER_AI_PROVIDER`, `XMANAGER_AI_API_KEY`, etc.

## Architecture

```
┌─────────────────────────────────────────────┐
│                XManager TUI                 │
│          (Go + Bubble Tea + Lip Gloss)      │
├──────────┬──────────┬───────────┬───────────┤
│ SSH      │ AI       │ Storage   │ Notify    │
│ Client   │ Module   │ SQLite    │ Telegram  │
│ Pool     │ OpenAI   │ GORM      │           │
│ Executor │ Claude   │           │           │
│ SFTP     │ Ollama   │           │           │
├──────────┴──────────┴───────────┴───────────┤
│            SSH Transport Layer              │
│        (golang.org/x/crypto/ssh)            │
├─────────────────────────────────────────────┤
│              Remote Server                  │
│    (Docker, PM2, Nginx, PostgreSQL, ...)    │
└─────────────────────────────────────────────┘
```

**Key Principle:** XManager runs on your local machine. All server interactions happen over SSH. Nothing is installed on the managed server.

## Project Structure

```
xmanager/
├── cmd/xmanager/         # CLI entry point (Cobra)
├── internal/
│   ├── ai/               # AI providers (OpenAI, Anthropic, Ollama)
│   ├── backup/           # Backup scheduler and runner
│   ├── config/           # Viper config + AES-256-GCM crypto
│   ├── dbmanager/        # Database management (Postgres, MySQL, MongoDB)
│   ├── docker/           # Docker container/compose management
│   ├── errtrack/         # Error tracking engine
│   ├── notify/           # Notification providers (Telegram)
│   ├── pm2/              # PM2 process management
│   ├── proxy/            # Nginx/Traefik management
│   ├── recon/            # Server reconnaissance
│   ├── ssh/              # SSH client, executor, SFTP, pool
│   ├── storage/          # SQLite + GORM models
│   └── tui/              # Bubble Tea screens and components
├── wizards/              # YAML wizard definitions
├── .github/workflows/    # CI/CD pipelines
├── Makefile
├── Dockerfile
└── .goreleaser.yml
```

## CLI Commands

```bash
xmanager              # Launch TUI (default)
xmanager connect my-server  # Connect to server directly
xmanager setup        # Run first-time setup wizard
xmanager version      # Print version info
xmanager reset        # Reset configuration
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.

---

Built with care by [LYRACORP](https://github.com/lyracorp)
