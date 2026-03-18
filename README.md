# Musher CLI

Publish agent bundles to the [Musher Hub](https://musher.dev) registry.

Musher is the publishing companion to [Mush](https://github.com/musher-dev/mush) — while Mush loads and runs bundles locally, Musher handles creating, validating, and publishing them.

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

# Initialize a bundle manifest
musher init

# Validate your bundle
musher validate

# Publish to the registry
musher publish
```

## Commands

### Authentication
| Command | Description |
|---------|-------------|
| `musher login` | Authenticate with API key |
| `musher logout` | Clear stored credentials |
| `musher whoami` | Show current identity and publisher handles |

### Publishing
| Command | Description |
|---------|-------------|
| `musher init` | Initialize `musher.yaml` manifest |
| `musher validate` | Validate manifest and check assets |
| `musher pack` | Pack bundle into a local OCI artifact |
| `musher push` | Upload bundle to registry |
| `musher publish` | Validate, pack, and push in one step |
| `musher yank <ref> <version>` | Yank a published version (hidden from search, fetchable by digest) |

### Discovery
| Command | Description |
|---------|-------------|
| `musher search [query]` | Search the Musher Hub |
| `musher info <pub/slug>` | Show bundle details |
| `musher ls` | List your published bundles |

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

## `musher.yaml` Manifest

```yaml
apiVersion: musher.dev/v1alpha1
kind: Bundle
publisher: acme
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

## Development

```bash
task build        # Build binary
task check        # Run all quality checks
task check:test   # Run tests only
task fmt          # Format code
```

## License

MIT
