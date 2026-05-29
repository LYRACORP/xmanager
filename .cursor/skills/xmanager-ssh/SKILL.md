---
name: xmanager-ssh
description: >-
  XManager remote execution over SSH — executor, pool, SFTP, commands on
  managed servers. Use when running shell commands, file transfer, or
  internal/ssh and server-side integration without agents.
---

# XManager — SSH layer

## Architecture

- `ssh.Pool` — connection pool keyed by server ID (`internal/ssh/pool.go`)
- `ssh.Client` — session from user config (key/password/jump host)
- `ssh.Executor` — run commands on an active connection (`internal/ssh/executor.go`)
- `ssh.SFTP` — file operations when needed

Screens get `ctx.Pool` via `shared.AppContext`. After user connects from server list, `ctx.ServerID` is set.

## Executor API

| Method | Use |
|--------|-----|
| `Run(cmd)` | Full result: stdout, stderr, exit code |
| `RunCombined(cmd)` | Merged output string |
| `RunQuiet(cmd)` | Stdout only (common in TUI) |
| `Stream(cmd)` | Long-running / log tail |

```go
ex, ok := ctx.Pool.GetExecutor(ctx.ServerID)
if !ok { /* show "not connected" */ }
out := ex.RunQuiet(`docker ps --format '{{.Names}}' 2>/dev/null`)
```

## Rules

- Commands run **on the remote host** — assume Linux, bash, common tools (docker, systemctl, nginx, etc.).
- Prefer **read-only recon** before destructive ops; surface errors in TUI, don't panic.
- **Never** install XManager binaries or agents on the server.
- Domain packages (`docker`, `pm2`, `proxy`, …) wrap executor usage — extend those instead of duplicating SSH in screens when logic is reusable.

## Security (public repo)

- Do not log or hardcode credentials, private keys, or host-specific secrets in source.
- Config secrets live in user `~/.config/xmanager/` (encrypted fields via `internal/config/crypto.go`).
