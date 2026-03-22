# Contributing to Musher CLI

## Prerequisites

- **Go 1.26.1+** — see `GO_VERSION` in `Taskfile.yml` for the exact version used in CI
- **[Task](https://taskfile.dev/)** — task runner (replaces Make)
- **shellcheck** — must be on `PATH` for shell script linting (`task check:shell`)

## Development Setup

```bash
# Clone the repository
git clone https://github.com/musher-dev/musher-cli.git
cd musher-cli

# Install dependencies, tools (golangci-lint, govulncheck, shfmt, actionlint),
# and Lefthook git hooks
task setup

# Build
task build

# Run the full quality suite (fmt, lint, vuln, test, cross-compile)
task check
```

## Commit Conventions

This project uses [Conventional Commits](https://www.conventionalcommits.org/).

```
feat: add new command
fix: correct error handling
chore: update dependencies
docs: improve README
```

## Code Style

- Format with `task fmt`
- Lint with `task check:lint`
- All user-facing output through `output.FromContext(cmd.Context())`
- Use `CLIError` for user-facing errors
- No `fmt.Print*` in command files

## Testing

```bash
task check:test         # Run all tests
task check:test-cover   # Run with coverage
```
