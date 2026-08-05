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

**From source** (requires Go 1.26.2+):

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

## Status

The Musher platform moved from a bundle registry to a deployment product. This
CLI is being rebuilt to match. The bundle and hub commands have been removed:
their backend endpoints were deleted (platform ADR 0062) and returned `404`.

Deployment commands (`musher deploy`, `status`, `logs`) are landing in
subsequent releases. Today the CLI covers authentication, configuration, and
diagnostics.

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

## Contributing

See [CONTRIBUTING.md](.github/CONTRIBUTING.md) for development setup, building, testing, and contribution guidelines.

## License

[MIT](LICENSE)
