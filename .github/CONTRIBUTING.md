# Contributing to Musher CLI

## Development Setup

```bash
# Clone the repository
git clone https://github.com/musher-dev/musher-cli.git
cd musher-cli

# Install dependencies and tools
task setup

# Build
task build

# Run all checks
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
