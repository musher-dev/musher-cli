<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/musher-dev/brand/main/dist/logo/svg/musher-logo-lockup-horizontal-light-transparent.svg" />
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/musher-dev/brand/main/dist/logo/svg/musher-logo-lockup-horizontal-dark-transparent.svg" />
    <img alt="Musher CLI" src="https://raw.githubusercontent.com/musher-dev/brand/main/dist/logo/svg/musher-logo-lockup-horizontal-dark-transparent.svg" height="80" />
  </picture>
  <h3>Author, validate, and publish Musher bundles.</h3>

  <a href="https://github.com/musher-dev/musher-cli/actions/workflows/ci.yml"><img src="https://github.com/musher-dev/musher-cli/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI" /></a>
  <a href="https://github.com/musher-dev/musher-cli/releases"><img src="https://img.shields.io/github/v/release/musher-dev/musher-cli" alt="Release" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/musher-dev/musher-cli" alt="Go" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/musher-dev/musher-cli" alt="License" /></a>

  <p>
    <a href="https://docs.musher.dev">Documentation</a> ·
    <a href="https://hub.musher.dev">Musher Hub</a> ·
    <a href="https://discord.gg/SaVMzMgX2c">Discord</a>
  </p>
</div>

Musher is the CLI for bundle authors on [musher.dev](https://musher.dev). Use it to scaffold bundle projects, register assets, validate bundle definitions, publish immutable bundle versions to the Musher registry, and manage public Hub listings.

Bundles published with Musher can be loaded and run locally with [Mush](https://github.com/musher-dev/mush) or consumed programmatically via the [Python SDK](https://github.com/musher-dev/python-sdk) and [TypeScript SDK](https://github.com/musher-dev/typescript-sdk).

## Install

**macOS / Linux:**

```bash
curl -fsSL https://get.musher.dev | sh
```

**Windows (PowerShell):**

```powershell
irm https://get.musher.dev/install.ps1 | iex
```

**From source** (requires Go 1.26.1+):

```bash
go install github.com/musher-dev/musher-cli/cmd/musher@latest
```

## Quick Start

Authenticate and check your identity:

```bash
musher login
musher whoami
```

**New bundle project:**

```bash
musher init       # scaffolds musher.yaml, skill templates, README.md
musher validate
musher push
```

**Existing project:**

```bash
musher init --empty   # creates musher.yaml only
musher add --all      # discovers and registers conventional assets
musher validate
musher push
```

Optionally, list on the public Hub:

```bash
musher hub publish <namespace/slug>
```

## Registry vs Hub

- **Registry** — Where versioned bundles live. Private by default. Every `musher push` creates an immutable, content-addressable version.
- **Hub** — The public discovery layer at [hub.musher.dev](https://hub.musher.dev). A separate action (`musher hub publish` or `musher push --publish-to-hub`) creates a listing. Requires `description`, `readme`, and `license` or `licenseFile` in `musher.yaml`.

You can push bundles without listing them on the Hub.

## Core Concepts

- **Bundle** — A versioned package of assets (skills, agents, prompts, tools, configs) pushed to the Musher registry.
- **Asset** — A single file within a bundle (e.g., a skill markdown file, a prompt template, an agent spec).
- **Bundle definition** — The `musher.yaml` file that describes your bundle's metadata and assets.
- **Namespace** — The publishing identity under which bundles are published (e.g., `acme/my-bundle`).
- **Immutability** — Once a version is pushed, it cannot be changed or overwritten. Yanked versions are hidden from search but remain fetchable by content-addressable digest, so existing lockfiles continue to resolve.

## `musher.yaml` Bundle Definition

**Minimal (private) bundle:**

```yaml
name: My Skill Bundle
description: A helpful coding skill

namespace: acme
slug: my-skill
version: 1.0.0

assets:
  - id: my-skill
    src: skills/my-skill/SKILL.md
```

**Hub-ready (public) bundle:**

```yaml
name: Team Code Review
description: Consistent code review guidance for engineering teams

namespace: acme
slug: code-review
version: 0.1.0

visibility: public
readme: README.md
licenseFile: LICENSE
repository: https://github.com/acme/code-review-bundle
keywords:
  - code-review
  - engineering

assets:
  - id: code-review
    src: skills/code-review/SKILL.md
  - id: review-agent
    src: agents/review-agent/AGENT.yaml
    kind: agent
```

## Commands

### Authentication

| Command | Description |
|---------|-------------|
| `musher login` | Authenticate with API key |
| `musher logout` | Clear stored credentials |
| `musher whoami` | Show current identity and writable namespaces |

### Publishing

| Command | Description |
|---------|-------------|
| `musher init` | Scaffold a bundle project. Use `--empty` for definition only |
| `musher add [path...]` | Register assets in `musher.yaml`. Use `--all` to discover conventional assets |
| `musher validate` | Validate bundle definition and assets |
| `musher push` | Push bundle to the registry. Use `--publish-to-hub` to also create a Hub listing |
| `musher pull <ns/slug[:version]>` | Download a bundle. Use `-o` to extract to a directory |
| `musher yank <ns/slug:version>` | Yank a published version. Use `--reason` to record why |
| `musher unyank <ns/slug:version>` | Restore a yanked version |

### Hub

| Command | Description |
|---------|-------------|
| `musher hub search [query]` | Search bundles (omit query for recently updated bundles) |
| `musher hub info <namespace/slug>` | Show bundle details |
| `musher hub list <namespace>` | List bundles for a namespace |
| `musher hub categories` | List Hub categories |
| `musher hub publish <namespace/slug>` | Create or update a Hub listing |
| `musher hub deprecate <namespace/slug>` | Deprecate a bundle |
| `musher hub undeprecate <namespace/slug>` | Remove deprecation |

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
