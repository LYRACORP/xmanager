# Cursor Skills — XManager

Skills teach the AI project-specific rules with **minimal tokens**: they load only when relevant (not auto-injected).

## Usage

In Cursor chat, reference a skill by name or path, for example:

- `@xmanager` — start here for any task in this repo
- `@xmanager-tui-screen` — new or changed Bubble Tea screen
- `@xmanager-ssh` — remote commands / SSH layer
- `@xmanager-ai` — AI providers or chat context
- `@xmanager-wizard` — YAML wizards in `wizards/`
- `@xmanager-public-contrib` — before commit/PR (secrets, security)

Combine: `@xmanager @xmanager-tui-screen` when adding a screen.

## Token tips

1. One primary skill per task — avoid attaching all skills at once.
2. Ask for a **focused diff** (e.g. "only `internal/tui/screens/foo`").
3. Prefer `make test` over asking the agent to re-read the whole tree.

## Public repo

These skills are safe to publish — they contain no secrets. Follow `xmanager-public-contrib` for contributions.
