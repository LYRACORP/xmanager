---
name: xmanager-wizard
description: >-
  XManager setup wizards defined as YAML in wizards/. Use when adding or
  editing wizard steps, ubuntu-docker, lemp, server-hardening, or wizard TUI.
---

# XManager — wizards

## Location

`wizards/*.yaml` — loaded by wizard screen (`internal/tui/screens/wizard`).

## Schema

```yaml
name: "Human-readable title"
description: "What this wizard does"
target_os: "ubuntu"   # hint for compatibility
steps:
  - name: "Step title"
    description: "Shown in TUI"
    commands:
      - "shell command run over SSH"
    skip_allowed: true   # user can skip step
```

## Guidelines

- Commands are **remote shell** lines (often `sudo apt-get ...`); idempotent steps where possible.
- Match existing wizards: `ubuntu-docker.yaml`, `ubuntu-lemp.yaml`, `server-hardening.yaml`
- No secrets in YAML — use env vars on server or prompt user in TUI if needed later
- Test mentally on clean Ubuntu; avoid destructive steps without clear description

## TUI

Wizard screen executes steps via SSH executor; progress/errors shown per step. Changing YAML usually needs no Go changes unless new fields are required.
