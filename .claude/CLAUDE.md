# Musher CLI

## What This Is

Musher is the **publishing CLI** for the Musher Hub registry — it creates,
validates, and publishes agent bundles. It is the companion to
[Mush](https://github.com/musher-dev/mush), which loads and runs bundles locally.

**IS**: Bundle publisher, manifest validator, registry client.
**IS NOT**: Bundle runner, job executor, worker manager. That's Mush.

Think `docker push` vs `docker run` — Musher publishes, Mush consumes.

## Directory Overview

### CLI Entry — `cmd/musher/`

Flat Docker-style verb commands, flags, user interaction orchestration.

- `main.go` — Entry point, version injection, error handling
- `root.go` — Root command, persistent flags, flat verb registration
- `bootstrap.go` — Runtime configuration (logging, output modes)
- `errors.go` — CLI error rendering and health probes
- `helpers.go` — Shared helpers (newAPIClient, requireAuth)
- `login.go` — Authentication with API key
- `logout.go` — Clear stored credentials
- `whoami.go` — Show identity and publisher handles
- `init.go` — Initialize musher.yaml manifest
- `validate.go` — Validate manifest and check assets
- `pack.go` — Pack bundle into local OCI artifact
- `push.go` — Upload bundle to registry
- `publish.go` — Validate, pack, and push in one step
- `yank.go` — Yank a published version
- `search.go` — Search the Musher Hub
- `info.go` — Show bundle details
- `ls.go` — List published bundles
- `doctor.go` — Diagnostic checks
- `update.go` — Self-update
- `version.go` — Version display
- `completion.go` — Shell completions

### Internal Packages — `internal/`

- `auth/` — Credential storage (keyring + file fallback)
- `buildinfo/` — Build metadata
- `client/` — HTTP client for Musher API (hub + publishing endpoints)
- `config/` — Viper-based configuration
- `doctor/` — Diagnostic check framework
- `errors/` — CLIError type
- `manifest/` — musher.yaml reader/writer/validator
- `observability/` — Structured logging + telemetry
- `output/` — CLI output handling (colors, spinners, TTY detection)
- `paths/` — XDG-style path resolution
- `prompt/` — Interactive user prompts
- `safeio/` — Safe file I/O wrappers
- `terminal/` — TTY detection and capabilities
- `update/` — Self-update from GitHub Releases
- `validate/` — Input validation utilities

## Stable Code Patterns

**Output via context** — All user-facing output goes through `output.FromContext(cmd.Context())`.

**Flat verbs** — Commands are `musher <verb>`, not `musher <noun> <verb>`. Root command directly registers all verbs.

**Error handling** — Use `CLIError` from `internal/errors` for user-facing errors. Wrap lower-level errors with `fmt.Errorf("context: %w", err)`.

**No TUI** — Publishing is batch-oriented. No bubbletea, no tcell, no PTY.

**Shared namespace** — Both Musher and Mush share `~/.config/musher/`, keyring `dev.musher.musher`, env var `MUSHER_API_KEY`.

## Development

```bash
task check        # All quality checks (fmt + lint + vuln + test)
task build        # Build musher binary
task check:test   # Run tests only
task fmt          # Format code
```

## Quick Reference

- **Binary**: `musher`
- **Config dir**: `~/.config/musher/` (XDG)
- **State dir**: `~/.local/state/musher/` (XDG)
- **Credentials**: OS Keyring (`dev.musher.musher`), falls back to `~/.config/musher/api-key`
- **Logs**: `~/.local/state/musher/logs/musher.log` (default sink)
- **Pack cache**: `~/.cache/musher/pack/`
- **OCI store**: `~/.local/share/musher/oci/`
- **API endpoint**: `api.url` config key or `MUSHER_API_URL` env var
- **Auth**: `MUSHER_API_KEY` env var or `musher login`
