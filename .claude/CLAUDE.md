# Musher CLI

## What This Is

Musher is the **deployment CLI** for the Musher platform — it authenticates,
deploys container images, and observes running deployments.

**IS**: Intent delivery and observability. It resolves local context, submits
deployment intent, and streams progress back.
**IS NOT**: A control plane. Server-side policy and the deployment state machine
are the sole authority. The CLI never re-implements them.

## Platform Context (read this before changing the client)

The platform removed the bundle registry end-to-end in ADR 0062. `/v1/bundles/*`,
`/v1/namespaces/*/bundles/*`, and the OCI distribution proxy are **gone (404)**.
The bundle, hub, cache, and harness surfaces were removed from this CLI to match.
Do not reintroduce them; `task check:contracts` asserts they stay gone.

Domain model: **Organization → Environment → Blueprint (Components) → Deployment
→ Replicas**. There is no `Application` and no `Release`; a Deployment *is* the
app.

Key API facts that constrain the design:

- **Auth**: `Authorization: Bearer mush_{8}.{43}`. The identity endpoint is
  `GET /v1/organizations` — `/v1/publisher/me` and `/v1/runner/me` are removed.
  `/agent/v1/*` requires the AGENT audience and belongs to the host-agent, not us.
- **Most public routes are still JWT-only.** Only the organization router accepts
  API keys today; deployment/component/blueprint writes need `current_user.id` as
  actor. Tracked in platform issues #1778 (widened from #1521) and #1780.
- **Errors are RFC 9457** `application/problem+json`. The stable machine token is
  the trailing slug of `type`.
- **Images must be pinned.** The server enforces a *floating-tag denylist*
  (`latest, main, main-stable, master, stable, edge, nightly, dev, rolling`) plus
  bare refs — not a semver allowlist. Mirror the denylist exactly; a client
  stricter than the server rejects refs the platform accepts.
- **SSE is a two-step ticket flow**: mint (authenticated), then stream with
  `?ticket=` and **no** Authorization header. Tickets are single-use with a
  10-second TTL, so every reconnect must re-mint. Heartbeats are bare `: ping`
  *comment* frames every 15s. These routes are `include_in_schema=False`, so they
  cannot be code-generated.
- **`Idempotency-Key` is not accepted** on any deployment/component/blueprint
  write. Idempotency comes from natural keys (read-before-write, 409-tolerance).

## Directory Overview

### CLI Entry — `cmd/musher/`

- `main.go` — Entry point, version injection, error handling
- `root.go` — Root command, persistent flags, command registration
- `bootstrap.go` — Runtime configuration (logging, output modes)
- `errors.go` — CLI error rendering and health probes
- `helpers.go` — Shared helpers (API client construction, credential resolution)
- `auth.go`, `auth_login.go`, `auth_logout.go`, `auth_status.go`
- `config.go`, `config_get.go`, `config_list.go`, `config_set.go`, `config_profile.go`
- `doctor.go`, `update.go`, `version.go`, `completion.go`

### Internal Packages — `internal/`

#### Platform/Core (must not import output/prompt)

- `auth/` — Credential storage (keyring + file fallback), profile-scoped
- `buildinfo/` — Build metadata
- `client/` — HTTP client for the Musher API
- `config/` — Viper-based configuration, profiles, overrides
- `env/` — Centralized environment-variable access (single source of truth)
- `errors/` — CLIError with exit codes and hints
- `observability/` — Structured logging + telemetry
- `paths/` — XDG-style path resolution
- `safeio/` — Safe file I/O wrappers
- `terminal/` — TTY detection and capabilities
- `update/` — Self-update from GitHub Releases
- `validate/` — Input validation utilities
- `workflow/` — Transport-agnostic use-case layer (`Progress`, `Confirmer`)

#### Presentation

- `output/` — CLI output (colors, spinners, TTY detection)
- `prompt/` — Interactive prompts
- `doctor/` — Diagnostic framework (output-agnostic)

## Stable Code Patterns

**Output via context** — user-facing output goes through
`output.FromContext(cmd.Context())`.

**stdout is the answer, stderr is the story.** stdout carries only the requested
data (so `--json | jq` works); every diagnostic, spinner, and progress line goes
to stderr.

**Error handling** — use `CLIError` from `internal/errors`. Wrap lower-level
errors with `clierrors.Errorf("context: %w", err)` — `fmt.Errorf` is banned by
forbidigo.

**Distinguish 401 from 403.** A credential that authenticates but lacks a
permission is valid; reporting it as a bad key sends users to rotate a working
credential.

**Config overrides, not env mutation.** `--api-url`/`--api-key`/`--profile` flow
through `config.Overrides` into `config.LoadWithOverrides`. The API key is held
outside viper so `Config.Set` can never persist it to disk.

**Architecture boundaries** — enforced by depguard *and* `internal/policy`. Every
new `internal/` package must be added to the classification maps in
`internal/policy/policy_test.go` or the build fails. `cmd/musher` must not import
`net/http`.

**Retired config keys** are reported, never silently ignored — see
`config.RetiredKeyReason`.

## Development

```bash
task check          # All quality checks (fmt + lint + vuln + test)
task build          # Build musher binary
task dev:install    # Build with version metadata and install to GOBIN
task check:test     # Run tests only
task check:contracts # Assert the command tree (incl. that bundle/hub stay gone)
task fmt            # Format code
```

## Quick Reference

- **Binary**: `musher`
- **Config dir**: `~/.config/musher/` (XDG)
- **Credentials**: OS Keyring (`musher/{hostname}`, user `api-key[@profile]`),
  falls back to `~/.local/share/musher/credentials/{hostID}[/{profile}]/api-key`
- **Logs**: `~/.local/state/musher/logs/musher.log`
- **API endpoint**: `api.url` config key or `MUSHER_API_URL`
- **Auth**: `MUSHER_API_KEY` env var or `musher auth login`
- **Exit codes**: 0 ok, 1 general, 2 auth, 3 network, 4 config, 5 timeout,
  6 execution, 64 usage. 7 is retired and must never be reassigned.
