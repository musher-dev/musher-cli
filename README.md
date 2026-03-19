# Musher CLI

Publish agent bundles to the [Musher Hub](https://musher.dev) registry.

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

# Publish to the registry
musher publish
```

## Core Concepts

- **Bundle** — A versioned package of assets (skills, prompts, configs) published to the Musher Hub.
- **Asset** — A single file within a bundle (e.g. a skill markdown file, a prompt template).
- **Bundle definition file** — The `musher.yaml` file that describes your bundle's metadata and assets.
- **Namespace** — The publishing identity under which bundles are published (e.g. `acme/my-bundle`).

## `musher.yaml` Bundle Definition

```yaml
kind: Bundle
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
    kind: skill
    installs:
      - harness: claude-code
        path: .claude/skills/my-skill/SKILL.md
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
| `musher publish` | Validate and publish the bundle |
| `musher yank <ns/slug:version>` | Yank a published version (hidden from search, fetchable by digest) |
| `musher unyank <ns/slug:version>` | Restore a yanked version |

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
- **OCI store**: `~/.local/share/musher/oci/`
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
