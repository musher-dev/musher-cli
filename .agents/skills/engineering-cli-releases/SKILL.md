---
name: engineering-cli-releases
description: >-
  Design release engineering for CLI applications including cross-compilation
  strategies, monorepo-aware tag prefixing, path-filtered CI pipelines,
  binary distribution (GitHub Releases, Homebrew, package managers), version
  injection via build flags, changelog generation, and independent release
  cycles for polyglot repositories. Use when setting up CLI release automation,
  configuring cross-compilation, designing monorepo release tagging, or
  planning CLI distribution channels.
  Triggered by: CLI release, cross compilation, release engineering, monorepo
  release, tag prefix, CLI distribution, Homebrew, binary release, changelog,
  version injection, GoReleaser, release pipeline CLI, CI release.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# CLI Release Engineering

Guidance for automating the build, versioning, and distribution of CLI binaries — especially in monorepo environments where multiple applications share a repository.

## Purpose

Releasing a CLI binary is fundamentally different from deploying a web service. Binaries must be compiled for multiple OS/architecture combinations, distributed through package managers, and versioned independently when sharing a repository with other applications. A manual release process invites human error; an automated pipeline ensures every release is reproducible, cross-platform, and traceable.

Use this skill when setting up release automation for a CLI, configuring monorepo-aware tagging, planning distribution channels, or designing version injection.

---

## Cross-Compilation Matrix

### Standard Targets

| OS | Architecture | Priority | Notes |
|----|-------------|----------|-------|
| Linux | amd64 | Must | Most CI/CD environments, servers |
| Linux | arm64 | Must | ARM servers, Raspberry Pi, Graviton |
| macOS | amd64 | Must | Intel Macs (declining but still common) |
| macOS | arm64 | Must | Apple Silicon (M-series) |
| Windows | amd64 | Should | Desktop development, Windows servers |
| Windows | arm64 | Nice to have | Surface Pro X, ARM Windows |

### Build Matrix Configuration

```yaml
targets:
  - os: linux
    arch: [amd64, arm64]
  - os: darwin
    arch: [amd64, arm64]
  - os: windows
    arch: [amd64]
```

### Binary Naming Convention

```
{binary}_{version}_{os}_{arch}.{ext}

Examples:
  mycli_1.2.0_linux_amd64.tar.gz
  mycli_1.2.0_darwin_arm64.tar.gz
  mycli_1.2.0_windows_amd64.zip
```

| Rule | Convention |
|------|-----------|
| Archive format (Linux/macOS) | `.tar.gz` |
| Archive format (Windows) | `.zip` |
| Include README and LICENSE in archive | Always |
| Checksum file | `checksums.txt` (SHA256) |

---

## Version Injection

### Build-Time Injection

Inject version, commit hash, and build date at compile time rather than maintaining a version file:

```
build flags:
  version = git tag (e.g., "1.2.0")
  commit  = git rev-parse HEAD (short hash)
  date    = build timestamp (ISO 8601)
```

### Version Output

```
$ mycli version
mycli 1.2.0 (commit: a1b2c3d, built: 2025-03-15T10:30:00Z)
```

### Version Information Struct

```
VersionInfo:
  version: string    # Semantic version from git tag
  commit: string     # Short commit hash
  date: string       # Build timestamp
  os: string         # Runtime OS
  arch: string       # Runtime architecture
```

Include `--version` as a root-level flag and `version` as a subcommand:

```
mycli --version          # Short: "mycli 1.2.0"
mycli version            # Long: full version info
mycli version --json     # Machine-readable version details
```

---

## Monorepo Release Tagging

### The Problem

In a monorepo with multiple applications, a plain `v1.0.0` tag is ambiguous — does it refer to the CLI, the backend, or the SDK?

### Scoped Tag Strategy

Prefix tags with the application name:

| Application | Tag Pattern | Example |
|-------------|-------------|---------|
| CLI | `cli/v*` | `cli/v1.2.0` |
| Backend | `backend/v*` | `backend/v3.0.1` |
| SDK | `sdk/v*` | `sdk/v0.5.0` |

### Tag Rules

| Rule | Rationale |
|------|-----------|
| Prefix matches the app directory name | Easy to correlate tags with code |
| Semver after the prefix | Standard version semantics |
| Release tooling strips the prefix | Binary reports `1.2.0`, not `cli/v1.2.0` |
| Each app tags independently | CLI v1.2.0 and Backend v3.0.1 can coexist |

### CI Workflow Trigger

```yaml
# Only triggers on CLI tags
on:
  push:
    tags: ['cli/v*']
```

---

## Path-Filtered CI Pipelines

### Problem

Running the full test suite for every application on every commit wastes CI resources and slows feedback loops. A documentation change should not trigger a 10-minute Go build.

### Solution: Path Filters

Configure CI to run jobs only when relevant files change:

```yaml
# Pseudocode for path filter
paths:
  cli-changed:
    - 'apps/cli/**'
    - 'libs/shared/**'        # Shared libraries affect the CLI
    - '.github/workflows/cli-*'  # Workflow changes
  
  cli-not-changed:
    - 'docs/**'
    - 'apps/backend/**'
    - '*.md'
```

### Pipeline Stages

| Stage | Trigger | Purpose |
|-------|---------|---------|
| **Lint** | Any CLI file change | Formatting, static analysis |
| **Test** | Any CLI file change | Unit + integration tests |
| **Build** | Any CLI file change | Compile for all targets |
| **Release** | `cli/v*` tag push | Build, package, distribute |

### Release Pipeline Steps

1. **Checkout** with full history (for changelog generation)
2. **Setup** language toolchain (pin version)
3. **Test** the full suite one final time
4. **Build** cross-compiled binaries
5. **Package** archives with checksums
6. **Publish** to GitHub Releases
7. **Update** package manager formulae (Homebrew, Scoop, etc.)

---

## Distribution Channels

### GitHub Releases (Primary)

Every tag push creates a GitHub Release with:
- Cross-compiled binary archives
- SHA256 checksum file
- Auto-generated changelog from commit messages
- Installation instructions in release notes

### Homebrew (macOS/Linux)

```ruby
# Formula (auto-generated by release tool)
class Mycli < Formula
  desc "CLI for managing the MyPlatform"
  homepage "https://github.com/org/repo"
  version "1.2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/.../mycli_1.2.0_darwin_arm64.tar.gz"
      sha256 "abc123..."
    else
      url "https://github.com/.../mycli_1.2.0_darwin_amd64.tar.gz"
      sha256 "def456..."
    end
  end
end
```

### Distribution Channel Matrix

| Channel | Platforms | Auto-Update | Setup Effort |
|---------|-----------|-------------|-------------|
| GitHub Releases | All | No (manual download) | Low |
| Homebrew tap | macOS, Linux | Yes (`brew upgrade`) | Medium |
| Scoop bucket | Windows | Yes (`scoop update`) | Medium |
| APT/YUM repo | Linux | Yes (system package manager) | High |
| Docker image | All (with Docker) | Via image tag | Low |
| npm / pip wrapper | All (with runtime) | Yes (`npm update -g`) | Medium |
| Install script (`curl \| sh`) | macOS, Linux | No | Low |

### Install Script Pattern

```bash
#!/bin/sh
# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

# Download and install
VERSION="latest"
URL="https://github.com/org/repo/releases/${VERSION}/download/mycli_${OS}_${ARCH}.tar.gz"
curl -sL "$URL" | tar xz -C /usr/local/bin mycli
```

---

## Changelog Generation

### Commit Convention

Use Conventional Commits for automatic changelog generation:

| Prefix | Changelog Section |
|--------|-------------------|
| `feat:` | Features |
| `fix:` | Bug Fixes |
| `docs:` | Documentation (usually excluded from changelog) |
| `perf:` | Performance Improvements |
| `refactor:` | Code Refactoring (usually excluded) |
| `BREAKING CHANGE:` | Breaking Changes (highlighted) |

### Changelog Format

```markdown
## v1.2.0 (2025-03-15)

### Features
- Add `project env` subcommand for environment variable management
- Support YAML output format (`-o yaml`)

### Bug Fixes
- Fix profile switching when config directory doesn't exist
- Correct exit code for authentication failures (now exits 3)

### Breaking Changes
- Rename `--format` flag to `--output` for consistency
```

---

## Release Checklist

- [ ] All tests pass on the release branch
- [ ] Version tag follows the scoped convention (`cli/v1.2.0`)
- [ ] Changelog covers all changes since last release
- [ ] Cross-compilation succeeds for all targets
- [ ] Checksum file is generated and included
- [ ] GitHub Release is created with artifacts
- [ ] Package manager formulae are updated (if applicable)
- [ ] Installation docs reference the new version
- [ ] `mycli version` reports the correct version string
