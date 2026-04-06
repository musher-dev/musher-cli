---
name: designing-terminal-interfaces
description: >-
  Design terminal user interfaces for CLI applications including interactive
  vs script mode detection (isatty), progressive enhancement strategy, TUI
  framework patterns (Model-View-Update / Elm Architecture), declarative
  terminal styling, output format switching (table, JSON, YAML), spinner and
  progress indicators, and color/accessibility handling. Use when designing
  CLI output behavior, adding interactive TUI elements, implementing output
  format switching, or making a CLI work for both humans and automation.
  Triggered by: terminal UI, TUI, interactive mode, script mode, isatty,
  CLI output, output format, spinner, progress bar, terminal styling, color
  output, table output, JSON output, Elm Architecture terminal.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# Terminal Interface Design

Guidance for building CLI output and interaction that is beautiful for humans and robust for automation.

## Purpose

A modern CLI must serve two audiences simultaneously: humans interacting in a terminal and scripts piping output into other programs. The approach is "progressive enhancement" — detect the context and adapt. Interactive users get rich TUI elements (spinners, color, tables); piped output gets machine-parseable data (JSON, plain text).

Use this skill when designing a CLI's output strategy, adding interactive elements, implementing output format switching, or ensuring a CLI works correctly in both interactive and scripted contexts.

---

## Progressive Enhancement Strategy

### Detection

| Check | How | Interpretation |
|-------|-----|---------------|
| Is stdout a terminal? | `isatty(stdout)` | If yes: interactive mode. If no: script mode |
| Is stderr a terminal? | `isatty(stderr)` | If yes: progress/spinners go to stderr |
| Is `NO_COLOR` set? | `env NO_COLOR` | If yes: disable all color output |
| Is `TERM=dumb`? | `env TERM` | If yes: disable advanced terminal features |
| Is `--no-color` flag set? | Flag check | Explicit user override |

### Behavior Matrix

| Feature | Interactive | Script/Pipe |
|---------|------------|-------------|
| Colors | Yes (unless NO_COLOR) | No |
| Spinners / progress bars | Yes (on stderr) | No |
| Tables with borders | Yes | No (tab-separated or JSON) |
| Interactive prompts | Yes | Error: "requires interactive terminal" |
| Animations | Yes | No |
| JSON output | On request (`-o json`) | Default |
| Unicode glyphs | Yes (checkmarks, arrows) | ASCII fallback |

### Implementation Rule

```
if isatty(stdout):
    mode = INTERACTIVE
    default_format = TABLE
else:
    mode = SCRIPT
    default_format = JSON
```

The `--output` / `-o` flag always overrides the detected default.

---

## Output Format System

### Supported Formats

| Format | Flag | Use Case | Content |
|--------|------|----------|---------|
| `table` | `-o table` | Human reading in terminal | Formatted columns with headers |
| `json` | `-o json` | Script consumption, `jq` piping | Structured JSON |
| `yaml` | `-o yaml` | Human reading of complex structures | Indented YAML |
| `text` | `-o text` | Simple values, one-per-line | Plain strings, no formatting |
| `csv` | `-o csv` | Spreadsheet import | Comma-separated with headers |

### Format Implementation Pattern

Separate data retrieval from rendering. The service layer returns structured data; the output layer renders it:

```
result = service.list_projects()

switch output_format:
    case TABLE:  render_table(result, columns=["name", "status", "created"])
    case JSON:   render_json(result)
    case YAML:   render_yaml(result)
    case TEXT:   render_text(result, field="name")
```

### Table Design Rules

| Rule | Example |
|------|---------|
| Column headers are UPPERCASE | `NAME  STATUS  CREATED` |
| Right-align numeric columns | `  42` not `42  ` |
| Truncate long values with ellipsis | `my-really-long-proj...` |
| Show empty state, not empty table | `No projects found.` |
| Respect terminal width | Truncate or omit columns if too narrow |

---

## TUI Patterns

### Model-View-Update (Elm Architecture)

For interactive flows (wizards, selections, forms), use the Elm Architecture pattern:

```
Model:
    - Current state (form fields, selection index, loading status)
    - Derived state (validation errors, filtered lists)

Update(message) -> Model:
    - KeyPress("up")    -> decrement selection index
    - KeyPress("enter") -> submit current selection
    - APIResponse(data) -> update data, clear loading flag
    - Error(err)        -> set error message

View(model) -> string:
    - Render the current state to a terminal string
    - Use styling abstractions for colors, borders, margins
```

### When to Use TUI

| Scenario | TUI? | Alternative |
|----------|------|-------------|
| Multi-step setup wizard | Yes | Flags with validation |
| Selection from a list | Yes | `--name` flag with autocomplete |
| Real-time log streaming | Yes (partial) | `tail -f` style plain output |
| Form with multiple fields | Yes | Individual flags per field |
| Simple confirmation | Yes (y/n prompt) | `--yes` / `--force` flag |

### TUI Exit Handling

- **Ctrl+C:** Cancel and exit cleanly (no stack trace)
- **Ctrl+D:** Same as Ctrl+C for input prompts
- **Escape:** Go back one step (in multi-step flows)
- **q:** Quit (in view-only TUI screens, not input fields)

---

## Spinner and Progress Patterns

### Spinner (Indeterminate Progress)

Use when an operation has unknown duration:

```
⠋ Deploying to staging...
⠙ Deploying to staging...
⠸ Deploying to staging... (12s)
✓ Deployed to staging (15s)
```

### Progress Bar (Determinate Progress)

Use when total work is known:

```
Uploading artifacts [████████░░░░░░░░] 52% (26/50 files)
```

### Rules

| Rule | Rationale |
|------|-----------|
| Spinners go to stderr | Keeps stdout clean for piped output |
| Show elapsed time after 5 seconds | Indicates the CLI hasn't hung |
| Replace spinner with result on completion | Final state: checkmark or X |
| Disable in non-interactive mode | Scripts don't need animation frames |
| Support `--quiet` to suppress all progress | Automation friendliness |

---

## Color and Accessibility

### Color Usage

| Purpose | Color | When |
|---------|-------|------|
| Success | Green | Operation completed successfully |
| Error | Red | Operation failed |
| Warning | Yellow | Non-fatal issue |
| Info / emphasis | Cyan or Bold | Key values, resource names |
| Muted / secondary | Gray / Dim | Timestamps, IDs, metadata |

### NO_COLOR Standard

Respect the `NO_COLOR` environment variable (https://no-color.org/):

1. If `NO_COLOR` is set (any value, including empty string): disable all color
2. If `--no-color` flag is passed: disable all color
3. If `TERM=dumb`: disable all color
4. If stdout is not a terminal: disable all color

### Accessibility Rules

- Never convey information through color alone (use symbols too: checkmark, X, warning triangle)
- Ensure sufficient contrast (avoid light colors on light backgrounds)
- Provide a `--no-color` flag as explicit override
- Test with `TERM=dumb` to verify the CLI is usable without color

---

## Error Output Design

### Structure

Errors go to stderr, always:

```
Error: project "my-project" not found

  The project may have been deleted or you may not have access.
  Run 'mycli project list' to see available projects.
```

### Error Format Rules

| Element | Rule |
|---------|------|
| First line | `Error: <short description>` |
| Detail | Indented, explains what went wrong |
| Suggestion | Indented, actionable next step |
| Exit code | Non-zero (1 for general, 2 for usage, specific codes for categories) |
| Verbose mode | Include request ID, timestamp, stack trace with `--verbose` |

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Usage error (invalid flags, missing arguments) |
| 3 | Authentication error |
| 4 | Not found |
| 5 | Permission denied |
| 126 | Command found but not executable |
| 127 | Command not found |
