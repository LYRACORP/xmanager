# Contributing to XManager TUI

Thank you for your interest in contributing to XManager! This document provides guidelines and setup instructions.

## Development Setup

### Prerequisites

- Go 1.22 or later
- GCC (for SQLite CGO)
- Git

### Clone and Build

```bash
git clone https://github.com/lyracorp/xmanager.git
cd xmanager
go mod download
make build
```

### Run Locally

```bash
make run
# or directly:
go run ./cmd/xmanager
```

### Run Tests

```bash
make test
```

### Lint

```bash
# Install golangci-lint first: https://golangci-lint.run/usage/install/
make lint
```

## Project Structure

The codebase follows Go standard project layout:

| Directory | Purpose |
|-----------|---------|
| `cmd/xmanager/` | CLI entry point |
| `internal/ai/` | AI provider interface + implementations |
| `internal/config/` | Config loading and encryption |
| `internal/storage/` | SQLite database and GORM models |
| `internal/ssh/` | SSH client, executor, SFTP, connection pool |
| `internal/tui/` | All Bubble Tea UI code |
| `internal/tui/screens/` | Individual screen packages |
| `internal/tui/components/` | Reusable TUI components |
| `internal/tui/theme/` | Lip Gloss theme system |
| `wizards/` | YAML wizard step definitions |

## Making Changes

### Adding a New Screen

1. Create a new package in `internal/tui/screens/<name>/`
2. Implement the `shared.Screen` interface (see `internal/tui/shared/types.go`)
3. Register the screen in `internal/tui/screens.go` and `internal/tui/app.go`
4. Add a `ScreenID` constant in `internal/tui/shared/types.go`

### Adding an AI Provider

1. Add a new file in `internal/ai/<provider>.go`
2. Implement the `Provider` interface from `internal/ai/provider.go`
3. Register in the `NewProvider` factory function

### Adding a Wizard

Create a YAML file in `wizards/` following this structure:

```yaml
name: "Wizard Name"
description: "What this wizard does"
target_os: "ubuntu"
steps:
  - name: "Step Name"
    description: "What this step does"
    commands:
      - "command to run"
    skip_allowed: true
```

## Code Style

- Follow standard Go conventions (`gofmt`, `goimports`)
- No unnecessary comments — code should be self-documenting
- Error messages should be lowercase and not end with punctuation
- Use `fmt.Errorf("context: %w", err)` for error wrapping

## Commit Messages

- Use present tense ("add feature" not "added feature")
- Keep the first line under 72 characters
- Reference issues when applicable: `fix: resolve SSH timeout (#42)`

## Pull Requests

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes and add tests
4. Ensure `make test` and `make lint` pass
5. Submit a pull request

## Security

If you discover a security vulnerability, please do NOT open a public issue. Email security@lyracorp.dev instead.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
