---
name: xmanager-public-contrib
description: >-
  Contribute safely to public lyracorp/xmanager — secrets, PRs, security
  disclosure. Use before commits, PRs, docs with credentials, or CI changes.
---

# XManager — public repository

## Never commit

- Real API keys (`sk-`, `sk-ant-`), Telegram `bot_token`, SSH passwords, private IPs/hostnames you own
- User config copies (`~/.config/xmanager/`), `.env`, key files, `id_rsa`
- Screenshots or logs with production data

Use placeholders: `your-api-key-here`, `example.com`, `192.0.2.1` (TEST-NET).

## Code & docs

- README examples may show **shape** of config; do not paste working credentials into PRs or issues.
- `internal/config/crypto.go` encrypts sensitive fields at rest — do not weaken crypto for convenience.
- Security issues: **do not** open public GitHub issues — email security@lyracorp.dev (see CONTRIBUTING.md).

## PR checklist

1. `make test` and `make lint` pass
2. Scope limited to the feature/fix
3. Commit messages: present tense, <72 char subject, optional `fix:` / `feat:` prefix
4. No force-push to `main`; no `--no-verify` unless maintainer asked

## AI agent behavior in this repo

- Do not read or echo user's local `~/.config/xmanager` contents into chat or patches.
- When unsure if data is secret, redact it.
- Do not add telemetry or phone-home code.
