# Musher CLI

Publish agent bundles to the [Musher](https://musher.dev) registry.

[Docs](https://docs.musher.dev) | [Hub](https://hub.musher.dev) | [Releases](https://github.com/musher-dev/musher-cli/releases)

Musher is the publishing companion to [Mush](https://github.com/musher-dev/mush) — while Mush loads and runs bundles locally, Musher handles creating, validating, and publishing them. Think `docker push` vs `docker run`.

## Install

```bash
# From GitHub Releases
curl -fsSL https://get.musher.dev | sh

# From source (requires Go 1.26.1+)
go install github.com/musher-dev/musher-cli/cmd/musher@latest
```

## Quick Start

```bash
musher login

musher init
# Creates: musher.yaml, skills/<slug>/SKILL.md, README.md
# Use --empty to create musher.yaml only

musher validate
musher push

# Optional: list on the public Hub catalog
musher hub publish <namespace/slug>
```

## Registry vs Hub

Musher has a two-step publishing model:

1. **`musher push`** uploads your bundle to the **registry** (private by default).
2. **`musher hub publish`** creates or updates a public **Hub catalog listing** for a pushed bundle.

You can push bundles without listing them on the Hub. Hub publishing requires additional metadata: `description`, `readme`, and `license` or `licenseFile`.

## Core Concepts

- **Bundle** — A versioned package of assets (skills, agents, prompts, tools, configs) pushed to the Musher registry.
- **Asset** — A single file within a bundle (e.g., a skill markdown file, a prompt template, an agent spec).
- **Bundle definition** — The `musher.yaml` file that describes your bundle's metadata and assets.
- **Namespace** — The publishing identity under which bundles are published (e.g., `acme/my-bundle`).

## `musher.yaml` Bundle Definition

**Minimal (private) bundle:**

```yaml
namespace: acme
slug: my-skill
version: 1.0.0
name: My Skill Bundle
description: A helpful coding skill
assets:
  - id: my-skill
    src: skills/my-skill/SKILL.md
```

**Hub-ready (public) bundle:**

```yaml
namespace: acme
slug: code-review
version: 0.1.0
name: Team Code Review
description: Consistent code review guidance for engineering teams
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
| `musher init` | Scaffold a bundle project (`musher.yaml`, skill template, `README.md`). Use `--empty` for definition only |
| `musher validate` | Validate bundle definition and assets |
| `musher push` | Push the bundle to the registry |
| `musher yank <ns/slug:version>` | Yank a published version |
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

## Development

Requires [Go 1.26.1+](https://go.dev/dl/) and [Task](https://taskfile.dev/).

```bash
task setup          # Download deps, install pinned tools, install hooks
task build          # Build binary
task check          # Full quality suite (fmt + lint + vuln + test)
task check:ci       # Canonical CI quality gate
task check:test     # Run tests only
task check:shell    # Lint shell scripts (requires shellcheck on PATH)
task check:workflow # Lint GitHub Actions workflows
task fmt            # Format Go and shell code, tidy modules
```

Local hooks are managed by [Lefthook](https://github.com/evilmartians/lefthook) and installed automatically by `task setup`.

## Contributing

See [CONTRIBUTING.md](.github/CONTRIBUTING.md).

## License

[MIT](LICENSE)
