---
name: designing-command-grammar
description: >-
  Design CLI command grammar including noun-verb vs verb-noun structure,
  subcommand grouping by resource, progressive discoverability, flag naming
  conventions, binary naming (ctl suffix pattern), and help text organization.
  Use when designing a new CLI command structure, evaluating command naming
  approaches, planning subcommand hierarchy, or naming a CLI binary.
  Triggered by: command grammar, noun-verb, verb-noun, subcommand, CLI naming,
  binary name, command structure, command hierarchy, CLI design, ctl suffix.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# Command Grammar Design

Guidance for designing intuitive, scalable CLI command structures that minimize cognitive load and maximize discoverability.

## Purpose

The "grammar" of a CLI — how commands, subcommands, and flags are organized — determines whether users can intuit the next command or must consult documentation for every operation. This skill covers the fundamental structural decisions that shape a CLI's usability as it grows from a handful of commands to dozens.

Use this skill when starting a new CLI project, refactoring an existing command tree, or evaluating whether a CLI's structure will scale with the platform it serves.

---

## Grammar Patterns

### Noun-Verb (Resource-Action)

Commands are grouped by the resource they operate on, with the action as a subcommand.

```
mycli project create
mycli project list
mycli project delete
mycli user invite
mycli user list
```

| Strength | Weakness |
|----------|----------|
| Groups related operations together | Deeper command tree (2+ levels) |
| Progressive discoverability via `--help` per resource | Requires more typing for simple operations |
| Scales well as resources and operations grow | Can feel verbose for small CLIs |
| Aligns naturally with RESTful API resources | |

**Best for:** Platform CLIs with multiple resource types, API-backed tools, tools expected to grow significantly.

**Examples in the wild:** `kubectl`, `gh`, `docker`, `aws`.

### Verb-Noun (Action-Resource)

Commands lead with the action, followed by the resource.

```
mycli create project
mycli list projects
mycli delete project
mycli invite user
```

| Strength | Weakness |
|----------|----------|
| Flat command tree, fewer levels | Root namespace clutters as operations multiply |
| Feels natural for small, focused tools | Help text becomes unreadable at scale |
| Action-first maps to user intent | Pluralization inconsistencies (list projects vs delete project) |

**Best for:** Small, single-purpose CLIs with fewer than 10 commands.

### Hybrid

Some CLIs use verb-noun for common operations and noun-verb for resource management.

```
mycli init                    # Top-level verb (common operation)
mycli login                   # Top-level verb (common operation)
mycli project create          # Noun-verb (resource management)
mycli project list            # Noun-verb (resource management)
```

**Best for:** CLIs that have a few high-frequency "global" commands alongside structured resource management.

---

## Decision Framework

Use this matrix to choose a grammar pattern:

| Factor | Noun-Verb | Verb-Noun | Hybrid |
|--------|-----------|-----------|--------|
| Number of resource types | 3+ | 1-2 | 3+ with common global actions |
| Expected growth | High | Low | High |
| API-backed | Yes | Either | Yes |
| Target audience | DevOps / platform engineers | End users / scripts | Mixed |
| Discoverability priority | High | Medium | High |

**Default recommendation:** Noun-Verb. It scales better and aligns with the resource-oriented mental model most developers already have from REST APIs and cloud CLIs.

---

## Binary Naming Conventions

The binary name is the first thing users type. It must be short, memorable, and collision-free.

### Suffix Patterns

| Suffix | Signal | Examples |
|--------|--------|----------|
| `ctl` | Controls a background system or platform | `kubectl`, `systemctl`, `journalctl` |
| `cli` | Generic CLI for a product | `vercel`, `railway` (no suffix needed if product name is short) |
| `sh` | Shell-like interactive tool | `ipython` (prefix, but same principle) |
| None | Product name is short enough | `gh`, `fly`, `wrangler` |

### Naming Rules

1. **Length:** 3-8 characters ideal. Users type this hundreds of times.
2. **Collision check:** Search `apt`, `brew`, `winget`, and `$PATH` for conflicts.
3. **Pronunciation:** Must be speakable in team conversations.
4. **Lowercase only:** Mixed case causes errors on case-sensitive file systems.
5. **No hyphens or underscores:** These require extra keystrokes. Prefer a single token.

### Evaluation Checklist

- [ ] Under 8 characters
- [ ] No collision with common system binaries or popular tools
- [ ] Pronounceable (can you say it in a sentence?)
- [ ] Conveys the product or platform identity
- [ ] Works across operating systems (no case sensitivity issues)

---

## Subcommand Organization

### Depth Limits

| Depth | Example | Guidance |
|-------|---------|----------|
| 1 | `mycli login` | Global actions, authentication, initialization |
| 2 | `mycli project create` | Standard resource operations — the sweet spot |
| 3 | `mycli project env set` | Acceptable for nested resources, but avoid if possible |
| 4+ | `mycli project env secret create` | Too deep. Flatten or use flags instead |

**Rule:** If you exceed depth 3, consider whether the nested noun should be a flag or a separate top-level resource.

### Standard Verbs

Use consistent verbs across all resources:

| Verb | Meaning | Idempotent? |
|------|---------|-------------|
| `create` | Create a new resource | No |
| `get` | Retrieve a single resource by ID | Yes |
| `list` | Retrieve a collection | Yes |
| `update` | Modify an existing resource | Yes (PUT semantics) |
| `delete` | Remove a resource | Yes |
| `describe` | Verbose detail view (richer than `get`) | Yes |

Avoid verb synonyms across resources (don't use `add` for one resource and `create` for another).

---

## Flag Conventions

### Naming Rules

| Rule | Good | Bad |
|------|------|-----|
| Kebab-case for long flags | `--output-format` | `--outputFormat`, `--output_format` |
| Single-letter shortcuts for common flags | `-o`, `-n`, `-v` | `-output` (ambiguous) |
| Boolean flags are positive by default | `--verbose` | `--no-quiet` |
| Negation uses `--no-` prefix | `--no-color` | `--color=false` (acceptable but verbose) |

### Reserved Flags

These flags should have consistent meaning across all commands:

| Flag | Short | Purpose |
|------|-------|---------|
| `--help` | `-h` | Show help text |
| `--version` | `-v` | Show binary version (root command only) |
| `--output` | `-o` | Output format (`json`, `yaml`, `table`, `text`) |
| `--verbose` | | Increase log verbosity |
| `--quiet` | `-q` | Suppress non-essential output |
| `--config` | `-c` | Path to config file |
| `--profile` | `-p` | Configuration profile name |

---

## Help Text Structure

Every command's help text should follow this structure:

```
<short description — one line>

Usage:
  mycli <command> [flags]

Available Commands:
  <command>    <description>

Flags:
  -h, --help   help for <command>

Use "mycli <command> --help" for more information about a command.
```

### Writing Good Descriptions

| Level | Example | Rule |
|-------|---------|------|
| Root | "Manage cloud resources from the terminal" | Product-level value proposition |
| Resource | "Manage projects and their configurations" | What the resource represents |
| Action | "Create a new project in the current organization" | Specific outcome of this command |

Descriptions should be imperative ("Create a new project") not declarative ("Creates a new project").
