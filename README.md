<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/musher-dev/brand/main/dist/logo/svg/musher-logo-lockup-horizontal-dark-transparent.svg" />
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/musher-dev/brand/main/dist/logo/svg/musher-logo-lockup-horizontal-light-transparent.svg" />
    <img alt="Musher CLI" src="https://raw.githubusercontent.com/musher-dev/brand/main/dist/logo/svg/musher-logo-lockup-horizontal-light-transparent.svg" height="80" />
  </picture>
  <h3>Deploy containers to the Musher platform.</h3>

  <a href="https://github.com/musher-dev/musher-cli/actions/workflows/ci.yml"><img src="https://github.com/musher-dev/musher-cli/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" /></a>
  <a href="https://github.com/musher-dev/musher-cli/releases"><img src="https://img.shields.io/github/v/release/musher-dev/musher-cli" alt="Release" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/musher-dev/musher-cli" alt="Go" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/musher-dev/musher-cli" alt="License" /></a>

  <p>
    <a href="https://docs.musher.dev">Documentation</a> ·
    <a href="https://discord.gg/SaVMzMgX2c">Discord</a>
  </p>
</div>

Musher is the CLI for [musher.dev](https://musher.dev). Use it to authenticate against the Musher platform and deploy containerized applications.

## Install

**macOS / Linux (or WSL):**

```bash
curl -fsSL https://get.musher.dev | sh
```

**Windows (PowerShell):**

```powershell
irm https://get.musher.dev/install.ps1 | iex
```

**From source** (requires Go 1.26.10+):

```bash
go install github.com/musher-dev/musher-cli/cmd/musher@latest
```

## What You'll Need

- **Musher CLI** — installed above.
- **A Musher account and API key** — create or manage keys at [console.musher.dev](https://console.musher.dev/settings/organization/api-keys).

## Quick Start

Authenticate and check your identity:

```bash
musher auth login
musher auth status
```

Verify your setup:

```bash
musher doctor
```

Build your repository and deploy it:

```bash
musher init      # detect the project, write musher.yaml with an explicit build recipe
musher deploy    # build, push, and deploy by digest; prints the URL
```

Run `musher` with no arguments in a terminal to open the interactive UI:
deployments, live logs, and guided init, build, and deploy screens.

## Building from Source Code

`musher build` and `musher deploy` build images on your machine and push them
to **your own registry** (GHCR, Docker Hub, …); the platform then deploys the
image pinned by digest.

- **Dockerfile** recipes build with BuildKit — Docker Engine's built-in BuildKit,
  or a standalone `buildkitd` via `build.buildkit_host` / `MUSHER_BUILDKIT_HOST`.
- **Buildpacks** recipes build with Cloud Native Buildpacks (default builder:
  Paketo `builder-jammy-base`, pinned by digest when `musher init` writes it).
  They need a Docker daemon.
- Images default to `linux/amd64`, the platform's host architecture, so builds
  on Apple Silicon still run. Override with `--platform`.
- Push credentials come from your Docker config (`docker login <registry>`).
  For a **private** image, add a pull credential for its registry in the Musher
  Console (Configuration → Registry Credentials); `musher deploy` warns when
  one is missing.

The recipe lives in `musher.yaml` and mirrors the component v2 `build` grammar;
it is used locally and never sent to the platform:

```yaml
apiVersion: musher.dev/v1
kind: App
metadata:
  name: web
spec:
  workload:
    kind: SERVICE
    build:
      repository: ghcr.io/acme/web   # where to push
      context: .
      dockerfile: Dockerfile          # or: buildpacks: {builderImage: …}
      arguments: {NODE_ENV: production}
      secrets: {NPM_TOKEN: {env: NPM_TOKEN}}   # RUN --mount=type=secret,id=NPM_TOKEN
    endpoints:
      http: {targetPort: 8080}
```

A build never guesses a recipe: `musher init` detects one and writes it down,
so what you review is what builds.

## Status

The Musher platform moved from a bundle registry to a deployment product. This
CLI is being rebuilt to match. The bundle and hub commands have been removed:
their backend endpoints were deleted (platform ADR 0062) and returned `404`.

The CLI covers building (`init`, `build`), deploying (`deploy`, `list`,
`status`, `logs`), authentication, configuration, and diagnostics.

## Core Concepts

- **Organization** — The billing and access-control boundary. An API key is bound
  to exactly one.
- **Environment** — An isolated execution context (for example `staging`,
  `production`) within an organization.
- **Deployment** — The deployable unit, and the app itself. It is created from a
  blueprint and runs a pinned container image.
- **Replica** — One running instance of a deployment. You scale deployments, not
  replicas.

## Commands

### Build

| Command | Description |
|---------|-------------|
| `musher init [dir]` | Detect the project and write `musher.yaml` with an explicit build recipe |
| `musher build [dir]` | Build and push the image; stdout is the pushed `repo:tag@sha256:…` |

### Deploy

| Command | Description |
|---------|-------------|
| `musher deploy [name]` | Deploy an image, or build from the recipe first; `--no-build --image …` skips the build |
| `musher list` | List deployments |
| `musher status <name>` | Show the state of a deployment |
| `musher logs <name>` | Read a deployment's logs |

### Authentication

| Command | Description |
|---------|-------------|
| `musher auth login` | Authenticate with an API key. `--no-verify` stores without a round trip |
| `musher auth logout` | Clear stored credentials |
| `musher auth status` | Show the authenticated identity and organization |

### Configuration

| Command | Description |
|---------|-------------|
| `musher config list` | List all configuration values |
| `musher config get <key>` | Read a configuration value |
| `musher config set <key> <value>` | Write a configuration value |
| `musher config profile` | Manage configuration profiles |

### Maintenance

| Command | Description |
|---------|-------------|
| `musher doctor` | Diagnostic checks |
| `musher update` | Self-update from GitHub Releases |
| `musher version` | Show version info |
| `musher completion` | Shell completions |

## Configuration

| Item | Location |
|------|----------|
| Config file | `~/.config/musher/config.yaml` |
| Credentials (keyring) | OS keyring, service `musher/<api-host>` |
| Credentials (fallback) | `~/.local/share/musher/credentials/<hostID>/api-key` |
| Logs | `~/.local/state/musher/logs/musher.log` |

**Build settings:** `build.repository` (default push target), `build.platform`
(default `linux/amd64`), `build.buildkit_host` (standalone BuildKit; otherwise
Docker's). Set them with `musher config set`.

**Auth precedence:** `MUSHER_API_KEY` env var / `--api-key` flag > OS keyring > credentials file.

**API endpoint:** `--api-url` flag > `MUSHER_API_URL` env var > `api.url` config key (default: `https://api.musher.dev`).

All paths follow XDG conventions. Override with `MUSHER_CONFIG_HOME`, `MUSHER_DATA_HOME`, `MUSHER_STATE_HOME`, or `MUSHER_HOME`.

### Global Flags

| Flag | Description |
|------|-------------|
| `--api-url` | Override the API endpoint |
| `--api-key` | Provide an API key directly (overrides keyring) |
| `--json` | Output results as JSON |
| `--quiet` | Minimal output (for CI) |
| `--no-color` | Disable colored output |
| `--no-input` | Disable interactive prompts |
| `--no-tui` | Print help instead of opening the interactive UI on a bare `musher` (or set `MUSHER_NO_TUI`) |

## Contributing

See [CONTRIBUTING.md](.github/CONTRIBUTING.md) for development setup, building, testing, and contribution guidelines.

## License

[MIT](LICENSE)
