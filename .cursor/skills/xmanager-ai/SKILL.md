---
name: xmanager-ai
description: >-
  XManager AI providers and chat context. Use when changing OpenAI, Anthropic,
  Ollama, internal/ai, chat screen, recon prompts, or server-aware AI context.
---

# XManager — AI module

## Provider interface (`internal/ai/provider.go`)

```go
type Provider interface {
    Chat(ctx, messages, opts...) (string, error)
    ChatStream(ctx, messages, out chan<- string, opts...) error
    Name() string
    ListModels(ctx) ([]string, error)
}
```

Factory: `NewProvider(ProviderConfig)` — types: `openai`, `anthropic`, `ollama`.

## Adding a provider

1. New file `internal/ai/<name>.go` implementing `Provider`
2. Register case in `NewProvider` switch
3. Wire config fields in `internal/config` (no real keys in repo — placeholders only)
4. Settings/chat screens read from `config.Config.AI`

## Server context (`internal/ai/context.go`)

`BuildSystemPrompt(ServerContext)` injects profile, resources, logs, alerts into system message. Recon/analyzer populate profile — keep prompts concise to limit tokens sent to APIs.

## Chat screen

`internal/tui/screens/chat/model.go` — uses provider + context; streaming via `ChatStream`.

## User config (not in git)

`~/.config/xmanager/config.yaml` or env: `XMANAGER_AI_PROVIDER`, `XMANAGER_AI_API_KEY`, etc. Never commit real keys.
