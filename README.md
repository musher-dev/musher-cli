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
apiVersion: musher.dev/v1alpha1
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
    src: skills/my-skill.md
    kind: skill
    installs:
      - harness: claude-code
        path: .claude/skills/my-skill.md
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
task build        # Build binary
task check        # Run all quality checks
task check:test   # Run tests only
task fmt          # Format code
```

## License

MIT
