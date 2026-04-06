---
name: designing-cli-configuration
description: >-
  Design CLI configuration systems including profile-based multi-environment
  switching, configuration file schema, precedence hierarchy (flags >
  environment variables > config file > defaults), XDG Base Directory
  compliance, and config file format selection. Use when designing a CLI config
  system, implementing profile switching, choosing config file locations, or
  establishing configuration precedence rules.
  Triggered by: CLI configuration, config profiles, config file, environment
  switching, XDG, config precedence, config schema, CLI settings, profile
  switching, multi-environment config.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# CLI Configuration Design

Guidance for designing configuration systems that handle multiple environments, respect platform conventions, and follow a clear precedence hierarchy.

## Purpose

A production CLI must work across local development, staging, and production environments — often within the same session. Configuration systems that support only a single flat file force users into manual file editing or environment variable gymnastics. Profile-based configuration, combined with a well-defined precedence hierarchy, eliminates this friction.

Use this skill when designing a new CLI's configuration system, adding multi-environment support, or establishing the rules for how flags, environment variables, and config files interact.

---

## Configuration Precedence Hierarchy

Values resolve top-down. The first source that provides a value wins:

| Priority | Source | Override Scope | Example |
|----------|--------|---------------|---------|
| 1 (highest) | Command-line flags | Per-invocation | `--api-url http://localhost:8000` |
| 2 | Environment variables | Per-session or per-process | `MYCLI_API_URL=http://localhost:8000` |
| 3 | Profile config file | Per-profile (persistent) | `api-url: http://localhost:8000` in config |
| 4 | Global config file | Per-user (persistent) | Default settings in `~/.config/mycli/` |
| 5 (lowest) | Compiled defaults | Hardcoded | Fallback values in source code |

### Rules

1. **Flags always win.** A user passing `--api-url` expects that value to be used regardless of what the config file says.
2. **Environment variables override files.** This enables CI/CD pipelines and containerized usage without config file management.
3. **Profile config overrides global config.** Users can maintain separate settings per environment.
4. **Defaults are last resort.** Hardcoded defaults should be sensible for the most common use case (often local development).

---

## Profile-Based Configuration

### Schema Design

```yaml
# ~/.config/mycli/config.yaml
current-profile: staging

profiles:
  production:
    api-url: "https://api.example.com"
    region: "us-east-1"
    output-format: "json"

  staging:
    api-url: "https://api.staging.example.com"
    region: "us-west-2"
    output-format: "table"

  local:
    api-url: "http://localhost:8000"
    region: "local"
    output-format: "table"
```

### Profile Resolution Order

1. `--profile` flag (if provided)
2. `MYCLI_PROFILE` environment variable
3. `current-profile` field in config file
4. First profile defined in the file (implicit default)

### Profile Management Commands

| Command | Purpose |
|---------|---------|
| `mycli config profile list` | List all profiles with active indicator |
| `mycli config profile use <name>` | Switch the active profile |
| `mycli config profile create <name>` | Create a new empty profile |
| `mycli config profile delete <name>` | Remove a profile |
| `mycli config set <key> <value>` | Set a value in the active profile |
| `mycli config get <key>` | Get a resolved value (shows effective source) |
| `mycli config show` | Display the fully resolved configuration |

### Implementation Pattern

Most configuration libraries do not support profiles natively. The standard approach:

1. Load the full config file
2. Read `current-profile` (or override from flag/env)
3. Extract the sub-tree at `profiles.<active-profile>`
4. Bind that sub-tree as the active configuration
5. Layer environment variables and flags on top

---

## Config File Location

### XDG Base Directory Compliance

On Linux and macOS, follow the XDG Base Directory Specification:

| Directory | Purpose | Default |
|-----------|---------|---------|
| `$XDG_CONFIG_HOME/mycli/` | Configuration files | `~/.config/mycli/` |
| `$XDG_DATA_HOME/mycli/` | Persistent data (caches, databases) | `~/.local/share/mycli/` |
| `$XDG_STATE_HOME/mycli/` | State data (logs, history) | `~/.local/state/mycli/` |

On Windows, use `%APPDATA%\mycli\` for configuration.

### File Search Order

1. `--config` flag (explicit path)
2. `$MYCLI_CONFIG` environment variable
3. `.mycli.yaml` in the current working directory (project-local override)
4. `$XDG_CONFIG_HOME/mycli/config.yaml` (user-level)
5. Built-in defaults

### First-Run Initialization

On first use, if no config file exists:

1. Create the config directory (`~/.config/mycli/`)
2. Write a minimal config with a `local` profile
3. Set `current-profile: local`
4. Print a message indicating where the config was created

---

## Config File Format Selection

| Format | Pros | Cons | Best For |
|--------|------|------|----------|
| YAML | Human-readable, supports comments, widely understood | Whitespace-sensitive, complex spec | User-edited config files |
| TOML | Explicit types, less ambiguous than YAML, supports comments | Less familiar to some developers | Structured config with clear sections |
| JSON | Universal parser support, strict | No comments, verbose | Machine-generated config, API responses |
| INI | Simple, familiar | No nesting, no types | Legacy or very simple config |

**Default recommendation:** YAML for user-facing config files. It supports comments (users can document their profiles) and is the most widely understood structured format.

---

## Environment Variable Naming

### Convention

Prefix all environment variables with the CLI name in uppercase, using underscores as separators:

```
MYCLI_API_URL=https://api.example.com
MYCLI_PROFILE=production
MYCLI_OUTPUT_FORMAT=json
MYCLI_VERBOSE=true
```

### Mapping Rules

| Config Key | Environment Variable |
|------------|---------------------|
| `api-url` | `MYCLI_API_URL` |
| `output-format` | `MYCLI_OUTPUT_FORMAT` |
| `profiles.prod.region` | `MYCLI_PROFILES_PROD_REGION` (if supported) |

### Nested Key Strategy

For nested configuration, choose one approach and document it:

1. **Flat mapping:** Only top-level keys have env var equivalents. Profile-specific values must come from the config file.
2. **Dot notation:** Support `MYCLI_PROFILES__PROD__REGION` (double underscore for nesting). More flexible but harder to document.

**Recommendation:** Flat mapping for simplicity. Users who need dynamic per-profile overrides can use `--profile` + `--api-url` flags.

---

## Configuration Validation

### At Load Time

Validate configuration when the CLI starts, not when values are used:

| Check | Action on Failure |
|-------|-------------------|
| Required fields missing | Error with specific field name and config file path |
| Invalid URL format | Error with the malformed value and expected format |
| Unknown profile name | Error listing available profiles |
| Deprecated field | Warning with migration instructions |
| Unknown fields | Warning (may indicate typo) |

### Validation Output

```
Error: invalid configuration in ~/.config/mycli/config.yaml
  - profiles.staging.api-url: "not-a-url" is not a valid URL
  - profiles.staging.region: required field is missing

Run 'mycli config show --profile staging' to see the full resolved config.
```

---

## Config Debugging

Provide a command that shows the fully resolved configuration with source attribution:

```
$ mycli config show --resolved

api-url: https://api.staging.example.com  (source: profile "staging")
region: us-east-1                          (source: environment MYCLI_REGION)
output-format: json                        (source: default)
profile: staging                           (source: flag --profile)
```

This eliminates the "where is this value coming from?" debugging loop.
