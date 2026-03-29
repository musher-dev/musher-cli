# Musher CLI

## What This Is

Musher is the **unified CLI** for the Musher platform — it creates,
validates, publishes, discovers, installs, and runs agent bundles.

**IS**: Bundle publisher, bundle consumer, hub explorer, harness coordinator.
**IS NOT**: Job executor (delegated to harnesses), worker orchestrator (connects to platform).

## Directory Overview

### CLI Entry — `cmd/musher/`

Grouped commands: `auth`, `bundle`, `hub`, `config` subcommands.

- `main.go` — Entry point, version injection, error handling
- `root.go` — Root command, persistent flags, verb registration
- `bootstrap.go` — Runtime configuration (logging, output modes)
- `errors.go` — CLI error rendering and health probes
- `helpers.go` — Shared helpers (newAPIClient, requireAuth, public client)
- `auth.go` — Auth parent command
- `auth_login.go` — Authentication with API key
- `auth_logout.go` — Clear stored credentials
- `auth_status.go` — Show authentication status and writable namespaces
- `bundle.go` — Bundle parent command
- `bundle_init.go` — Initialize musher.yaml bundle definition file
- `bundle_add.go` — Add assets to the bundle definition
- `bundle_remove.go` — Remove assets from the bundle definition
- `bundle_validate.go` — Validate bundle definition file and check assets
- `bundle_push.go` — Validate and push the bundle to the registry
- `bundle_pull.go` — Download a bundle from the registry
- `bundle_yank.go` — Yank a published version
- `bundle_unyank.go` — Restore a yanked version
- `bundle_globs.go` — Glob pattern expansion for asset paths
- `hub.go` — Hub parent command
- `hub_search.go` — Search hub bundles
- `hub_info.go` — Show hub bundle details
- `hub_list.go` — List bundles for a namespace
- `hub_categories.go` — List hub categories
- `hub_publish.go` — Publish a listing to the hub
- `hub_deprecate.go` — Deprecate a hub bundle
- `hub_undeprecate.go` — Remove deprecation from a hub bundle
- `config.go` — Config parent command
- `config_list.go` — List all configuration
- `config_get.go` — Get a configuration value
- `config_set.go` — Set a configuration value
- `doctor.go` — Diagnostic checks
- `update.go` — Self-update
- `version.go` — Version display
- `completion.go` — Shell completions

### Schemas — `schemas/`

- `bundledef/v1alpha1.json` — JSON Schema (Draft 2020-12) for musher.yaml bundle definitions
- `bundledef/embed.go` — Go embed wrapper for schema consumption

### Internal Packages — `internal/`

#### Platform/Core (no output/prompt dependency)

- `auth/` — Credential storage (keyring + file fallback)
- `buildinfo/` — Build metadata
- `client/` — HTTP client for Musher API (hub, publishing, and bundle endpoints)
- `config/` — Viper-based configuration
- `errors/` — CLIError type with exit codes and hints
- `bundledef/` — musher.yaml reader/writer/validator, bundle ref parsing
- `observability/` — Structured logging + telemetry
- `paths/` — XDG-style path resolution
- `safeio/` — Safe file I/O wrappers
- `terminal/` — TTY detection and capabilities
- `update/` — Self-update from GitHub Releases
- `validate/` — Input validation utilities

#### Presentation (CLI output layer)

- `output/` — CLI output handling (colors, spinners, TTY detection)
- `prompt/` — Interactive user prompts
- `doctor/` — Diagnostic check framework (output-agnostic)

#### Consumer (bundle usage and execution)

- `bundle/cache/` — Content-addressable bundle cache (SHA256 blobs)
- `bundle/install/` — Installation tracking (.musher/installed.json)
- `harness/` — Harness provider abstraction, spec parsing, registry
- `harness/provider/` — Individual harness provider implementations
- `tui/` — Terminal UI mode detection and bubbletea integration
- `transcript/` — Session recording (JSONL events)

#### Testing

- `testutil/` — Shared test helpers (HTTP mocks, env overrides, context builders)

## Stable Code Patterns

**Output via context** — All user-facing output goes through `output.FromContext(cmd.Context())`.

**Command shape** — Authentication uses `musher auth <verb>`, bundle authoring uses `musher bundle <verb>`, catalog operations use `musher hub <verb>`, and configuration uses `musher config <verb>`. Consumer commands (install, load, run, etc.) will be top-level verbs with `musher bundle <verb>` as fully-qualified aliases.

**Error handling** — Use `CLIError` from `internal/errors` for user-facing errors. Wrap lower-level errors with `fmt.Errorf("context: %w", err)`.

**TUI mode** — Interactive TUI (bubbletea) for discovery and guided flows when TTY is detected. Batch mode is default for non-TTY/CI. The `--no-tui` flag forces batch mode. When TUI is active, output must route through bubbletea message passing, not direct stdout writes.

**Shared namespace** — Uses `~/.config/musher/`, keyring `musher/{hostname}`, env var `MUSHER_API_KEY`.

**Architecture boundaries** — Enforced by depguard: internal packages cannot import cmd; platform/core packages cannot import output/prompt; consumer packages (bundle/*, harness, tui, transcript) cannot import presentation layer; client cannot import consumer packages.

**Harness providers** — Adding a new harness requires: `spec.yaml` (declarative config), `provider.go` (embeds spec, exports Module), `executor.go` (implements Executor interface), and one registration line in builtins.

## Development

```bash
task check          # All quality checks (fmt + lint + vuln + test)
task build          # Build musher binary
task dev:install    # Build with version metadata and install to GOBIN
task check:test     # Run tests only
task dev:test-live  # Integration tests against live API (requires MUSHER_API_KEY)
task fmt            # Format code
```

## Quick Reference

- **Binary**: `musher`
- **Config dir**: `~/.config/musher/` (XDG)
- **State dir**: `~/.local/state/musher/` (XDG)
- **Data dir**: `~/.local/share/musher/` (XDG)
- **Cache dir**: `~/.cache/musher/` (XDG)
- **Credentials**: OS Keyring (`musher/{hostname}`), falls back to `~/.local/share/musher/credentials/{hostID}/api-key`
- **Logs**: `~/.local/state/musher/logs/musher.log` (default sink)
- **API endpoint**: `api.url` config key or `MUSHER_API_URL` env var
- **Auth**: `MUSHER_API_KEY` env var or `musher auth login`
- **TUI control**: `--no-tui` flag, `tui.enabled` config key, or non-TTY detection
