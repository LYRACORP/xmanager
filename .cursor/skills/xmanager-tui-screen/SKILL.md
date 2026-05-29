---
name: xmanager-tui-screen
description: >-
  Add or modify XManager Bubble Tea screens. Use when creating screens,
  navigation, keyboard shortcuts, lipgloss UI, or internal/tui/screens changes.
---

# XManager — TUI screens

## Screen contract

Every screen implements `shared.Screen` in `internal/tui/shared/types.go`:

- `Init() tea.Cmd`, `Update(tea.Msg) (Screen, tea.Cmd)`, `View() string`
- `SetSize(width, height int)`, `Name() string`
- Constructor: `New(ctx *shared.AppContext) *Model`

## Registration checklist

1. Add `ScreenID` constant in `internal/tui/shared/types.go` (+ `String()` name in the names array)
2. Create `internal/tui/screens/<name>/model.go` (package name = folder name)
3. Factory in `internal/tui/screens.go`: `New<Name>Screen(ctx) shared.Screen`
4. Register in `internal/tui/app.go` → `initScreens()`

## Navigation

- **Forward**: return `func() tea.Msg { return shared.NavigateMsg{Screen: shared.ScreenX, ServerID: m.ctx.ServerID} }`
- **Back**: `shared.GoBackMsg{}` (app pops router stack and re-inits screen)
- App handles `ConnectServerMsg`, `ServerConnectedMsg`, window resize for all screens

## Async / SSH pattern

```go
type myDataMsg struct { data string; err error }

func (m *Model) load() tea.Cmd {
    return func() tea.Msg {
        ex, ok := m.ctx.Pool.GetExecutor(m.ctx.ServerID)
        if !ok { return myDataMsg{err: errors.New("not connected")} }
        out := ex.RunQuiet("command here")
        return myDataMsg{data: out}
    }
}
```

Handle custom msg types in `Update`; batch with `tea.Batch` in `Init`.

## UI conventions

- Theme: `internal/tui/theme` (respect `config.UI.Theme`)
- Reuse: `internal/tui/components` (table, modal, gauge, help, statusbar)
- List keys: `j`/`k` or arrows; `Enter` select; `Esc`/`q` back; `?` help
- Content height: app passes `height - 3` for help bar

## Reference implementation

Copy patterns from `internal/tui/screens/dashboard/model.go` (tick refresh, executor, lipgloss layout).
