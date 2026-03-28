# Musher CLI

## What This Is

Musher is the **publishing CLI** for the Musher Hub registry — it creates,
validates, and publishes agent bundles.

**IS**: Bundle publisher, bundle definition validator.
**IS NOT**: Bundle runner, job executor, worker manager.

## Directory Overview

### CLI Entry — `cmd/musher/`

Grouped commands: `auth`, `bundle`, `hub` subcommands.

- `main.go` — Entry point, version injection, error handling
- `root.go` — Root command, persistent flags, verb registration
- `bootstrap.go` — Runtime configuration (logging, output modes)
- `errors.go` — CLI error rendering and health probes
- `helpers.go` — Shared helpers (newAPIClient, requireAuth, public client)
- `auth.go` — Auth parent command
- `auth_login.go` — Authentication with API key
- `auth_logout.go` — Clear stored credentials
- `auth_status.go` — Show authentication status and writable namespaces
- `bundle.go` — Bundle parent command + `parseBundleRefOptionalVersion` helper
- `bundle_init.go` — Initialize musher.yaml bundle definition file
- `bundle_add.go` — Add assets to the bundle definition
- `bundle_remove.go` — Remove assets from the bundle definition
- `bundle_validate.go` — Validate bundle definition file and check assets
- `bundle_push.go` — Validate and push the bundle to the registry
- `bundle_pull.go` — Download a bundle from the registry
- `bundle_yank.go` — Yank a published version
- `bundle_unyank.go` — Restore a yanked version
- `bundle_globs.go` — Glob pattern expansion for asset paths
- `hub.go` — Hub parent command + `parseBundleRef` helper
- `hub_search.go` — Search hub bundles
- `hub_info.go` — Show hub bundle details
- `hub_list.go` — List bundles for a namespace
- `hub_categories.go` — List hub categories
- `hub_publish.go` — Publish a listing to the hub
- `hub_deprecate.go` — Deprecate a hub bundle
- `hub_undeprecate.go` — Remove deprecation from a hub bundle
- `doctor.go` — Diagnostic checks
- `update.go` — Self-update
- `version.go` — Version display
- `completion.go` — Shell completions

### Schemas — `schemas/`

- `bundledef/v1alpha1.json` — JSON Schema (Draft 2020-12) for musher.yaml bundle definitions
- `bundledef/embed.go` — Go embed wrapper for schema consumption

### Internal Packages — `internal/`

- `auth/` — Credential storage (keyring + file fallback)
- `buildinfo/` — Build metadata
- `client/` — HTTP client for Musher API (hub + publishing endpoints)
- `config/` — Viper-based configuration
- `doctor/` — Diagnostic check framework
- `errors/` — CLIError type
- `bundledef/` — musher.yaml reader/writer/validator (with JSON Schema validation)
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

**Command shape** — Authentication uses `musher auth <verb>`, bundle authoring uses `musher bundle <verb>`, and catalog operations use `musher hub <verb>`.

**Error handling** — Use `CLIError` from `internal/errors` for user-facing errors. Wrap lower-level errors with `fmt.Errorf("context: %w", err)`.

**No TUI** — Publishing is batch-oriented. No bubbletea, no tcell, no PTY.

**Shared namespace** — Uses `~/.config/musher/`, keyring `musher/{hostname}`, env var `MUSHER_API_KEY`.

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
- **Data dir**: `~/.local/share/musher/` (XDG)
- **Credentials**: OS Keyring (`musher/{hostname}`), falls back to `~/.local/share/musher/credentials/{hostID}/api-key`
- **Logs**: `~/.local/state/musher/logs/musher.log` (default sink)
- **API endpoint**: `api.url` config key or `MUSHER_API_URL` env var
- **Auth**: `MUSHER_API_KEY` env var or `musher auth login`
