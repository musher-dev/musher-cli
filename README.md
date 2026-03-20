# Musher CLI

Publish agent bundles to the [Musher](https://musher.dev) registry.

Musher is the publishing companion to [Mush](https://github.com/musher-dev/mush) — while Mush loads and runs bundles locally, Musher handles creating, validating, and publishing them. Think `docker push` vs `docker run`.

## Install

```bash
# From GitHub Releases
curl -fsSL https://get.musher.dev | sh

# Or with Go
go install github.com/musher-dev/musher-cli/cmd/musher@latest
```

## Quick Start

```bash
# Authenticate
musher login

# Initialize a bundle definition file
musher init

# Validate your bundle
musher validate

# Push to the registry
musher push
```

## Core Concepts

- **Bundle** — A versioned package of assets (skills, prompts, configs) published to the Musher Hub.
- **Asset** — A single file within a bundle (e.g. a skill markdown file, a prompt template).
- **Bundle definition file** — The `musher.yaml` file that describes your bundle's metadata and assets.
- **Namespace** — The publishing identity under which bundles are published (e.g. `acme/my-bundle`).

## `musher.yaml` Bundle Definition

```yaml
namespace: acme
slug: my-skill
version: 1.0.0
name: My Skill Bundle
description: A helpful coding skill
keywords:
  - productivity
  - coding
assets:
  - id: my-skill
    src: skills/my-skill/SKILL.md
```

Example skill file:

```md
---
name: my-skill
description: A helpful coding skill. Use when the task matches this bundle's specialization.
---

# My Skill

Add the instructions the agent should follow here.
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
| `musher init` | Create a `musher.yaml` bundle definition file |
| `musher validate` | Validate bundle definition file and assets |
| `musher push` | Validate and push the bundle to the registry |
| `musher import skills <path...>` | Import skills from local directories |
| `musher import npm --installed` | Import skills from installed npm packages |
| `musher yank <ns/slug:version>` | Yank a published version (hidden from search, fetchable by digest) |
| `musher unyank <ns/slug:version>` | Restore a yanked version |

### Hub
| Command | Description |
|---------|-------------|
| `musher hub search <query>` | Search for bundles on the Hub |
| `musher hub info <namespace/slug>` | Show details for a Hub bundle |
| `musher hub list <namespace>` | List bundles for a namespace |
| `musher hub categories` | List Hub categories |
| `musher hub publish <namespace/slug>` | Create or update a Hub listing |
| `musher hub deprecate <namespace/slug>` | Deprecate a Hub bundle |
| `musher hub undeprecate <namespace/slug>` | Remove deprecation from a Hub bundle |

> **Two-step flow:** `musher push` uploads your bundle to the registry. `musher hub publish` creates or updates the public catalog listing.

### Maintenance
| Command | Description |
|---------|-------------|
| `musher doctor` | Diagnostic checks |
| `musher update` | Self-update from GitHub Releases |
| `musher version` | Show version info |
| `musher completion` | Shell completions |

## Configuration

- **Config dir**: `~/.config/musher/`
- **Credentials**: OS Keyring (`dev.musher.musher`) or `~/.config/musher/api-key`
- **Logs**: `~/.local/state/musher/logs/musher.log`
- **Pack cache**: `~/.cache/musher/pack/`
- **API endpoint**: `MUSHER_API_URL` or `api.url` config key (default: `https://api.musher.dev`)
- **Auth**: `MUSHER_API_KEY` env var or `musher login`

## Development

```bash
task setup         # Download deps, install pinned tools, install hooks
task build         # Build binary
task check:ci      # Run the canonical quality gate
task check:test    # Run tests only
task check:shell   # Lint shell scripts
task check:workflow # Lint GitHub Actions workflows
task fmt           # Format Go and shell code, then tidy modules
```

Local hooks are managed by `lefthook` and are installed automatically by `task setup`.

## License

MIT
