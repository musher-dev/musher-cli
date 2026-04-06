---
name: architecting-cli-structure
description: >-
  Architect CLI application directory structure including hexagonal separation
  between interface and domain layers, encapsulation boundaries (internal vs
  public packages), entry point organization, service layer patterns, and
  monorepo placement strategies. Use when scaffolding a new CLI project,
  reviewing CLI code organization, or deciding where to place business logic
  vs command definitions.
  Triggered by: CLI structure, CLI architecture, project layout, internal vs
  pkg, hexagonal CLI, command layer, service layer, CLI scaffolding, monorepo
  CLI, CLI directory structure.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# CLI Application Structure

Guidance for organizing CLI application code into maintainable, testable layers that separate interface concerns from domain logic.

## Purpose

A CLI's internal architecture determines how easily it can be tested, extended, and maintained. The most common failure mode is coupling business logic directly to command definitions, making it impossible to reuse logic from a TUI, background task, or SDK without dragging along the CLI framework.

This skill provides patterns for structuring CLI applications using hexagonal architecture principles — regardless of language or framework.

---

## Hexagonal Architecture for CLIs

### The Two-Layer Model

| Layer | Responsibility | Contains | Does NOT contain |
|-------|---------------|----------|-----------------|
| **Interface Layer** | Command definitions, flag parsing, help text, output formatting | Command registration, flag binding, usage strings, output rendering | Business logic, API calls, data transformation |
| **Domain Layer** | Business logic, service orchestration, data transformation | Service functions, data types, validation rules, API client calls | CLI flags, framework types, output formatting |

### Why This Separation Matters

1. **Testability:** Domain logic can be unit-tested with plain function calls — no need to simulate CLI invocations.
2. **Reusability:** The same domain logic works from a CLI command, a TUI view, a background scheduler, or an SDK.
3. **Framework independence:** Swapping the CLI framework (or adding a TUI layer) only affects the interface layer.

---

## Recommended Directory Layout

```
cli-project/
├── cmd/                    # Interface Layer
│   └── appname/            # Binary entry point
│       ├── main.go         # Minimal: initialize and execute root command
│       └── (or main.ts, main.py, etc.)
├── internal/               # Domain Layer (encapsulated)
│   ├── service/            # Business logic services
│   ├── client/             # API client (generated or manual)
│   ├── config/             # Configuration loading and profiles
│   └── ui/                 # TUI components (if applicable)
├── pkg/                    # Public libraries (optional, use sparingly)
│   └── api/                # Exported types for external consumers
├── commands/               # Command definitions (alternative to cmd/)
│   ├── root.go             # Root command setup
│   ├── project.go          # Project subcommands
│   └── user.go             # User subcommands
└── go.mod / package.json / pyproject.toml
```

### Layout Decision Matrix

| Question | If Yes | If No |
|----------|--------|-------|
| Will other programs import your CLI's types? | Use `pkg/` for shared types | Keep everything in `internal/` |
| Is this a monorepo with multiple apps? | Nest under `apps/cli/` | Use root-level layout |
| Does the language enforce encapsulation (Go `internal/`)? | Leverage compiler enforcement | Use convention + code review |
| Do you have a TUI alongside the CLI? | Separate `internal/ui/` from `internal/service/` | Commands call services directly |

---

## Encapsulation Boundaries

### Internal (Private) Code

Code that should **never** be imported by external consumers:

- Command definitions and flag parsing
- Configuration loading and profile resolution
- Authentication flows and token management
- TUI components and interactive prompts
- Output formatting and rendering

### Public Code (Use Sparingly)

Code that external consumers may legitimately need:

- API client types (request/response structs)
- Error types and error codes
- Resource models (if the CLI doubles as a reference SDK)

**Default rule:** Everything goes in the private/internal directory until a concrete external consumer requires it. Premature exposure creates backward-compatibility obligations.

---

## Entry Point Pattern

The binary entry point should be minimal — its only job is to wire dependencies and execute the root command.

### Minimal Entry Point

```
func main():
    config = load_config()
    client = create_api_client(config)
    root_command = build_root_command(client, config)
    execute(root_command)
```

### Anti-Patterns

| Anti-Pattern | Problem | Fix |
|---|---|---|
| Business logic in main | Untestable, tightly coupled | Move to service layer |
| Global state for config/client | Hidden dependencies, test pollution | Pass as constructor arguments |
| Flag parsing in service layer | Service depends on CLI framework | Service accepts plain types |
| Output formatting in service layer | Service knows about terminal concerns | Service returns data; command formats |

---

## Command Registration Pattern

### One File Per Resource

Each resource (or logical grouping) gets its own file in the commands directory:

```
commands/
├── root.go          # Root command, global flags, version
├── project.go       # project create, project list, project delete
├── user.go          # user invite, user list, user remove
├── auth.go          # login, logout, whoami
└── config.go        # config set, config get, config list
```

### Command-to-Service Wiring

Each command function follows the same pattern:

1. **Parse flags** into a plain request struct
2. **Call the service** with the request struct
3. **Format the response** for the chosen output format
4. **Handle errors** with appropriate exit codes

```
function handle_project_create(flags):
    request = ProjectCreateRequest(
        name = flags.name,
        description = flags.description
    )
    result = project_service.create(request)
    render_output(result, flags.output_format)
```

---

## Monorepo Placement

### Nested Application Pattern

When the CLI lives in a monorepo alongside other applications:

```
monorepo/
├── apps/
│   ├── backend/          # API server
│   ├── cli/              # CLI application
│   │   ├── cmd/
│   │   ├── internal/
│   │   └── go.mod        # Self-contained module
│   └── web/              # Web frontend
├── libs/                 # Shared libraries
├── tools/                # Build tooling
└── go.work / pnpm-workspace.yaml / pyproject.toml
```

### Monorepo Rules

| Rule | Rationale |
|------|-----------|
| CLI has its own dependency manifest | Prevents dependency conflicts with other apps |
| CLI builds independently from its subdirectory | CI can `cd apps/cli && build` without workspace |
| Shared types go in `libs/`, not copied | Single source of truth for cross-app models |
| Workspace files are dev-only, not used in CI | CI verifies the module is self-contained |

---

## Dependency Injection for CLIs

### Constructor Injection Pattern

Services receive their dependencies through constructors, not global lookups:

```
// Service constructor
function new_project_service(client, config):
    return ProjectService(client=client, config=config)

// Command wiring (in interface layer)
function build_project_commands(service):
    create_cmd = Command("create", handler=lambda flags: service.create(...))
    list_cmd = Command("list", handler=lambda flags: service.list(...))
    return group("project", [create_cmd, list_cmd])
```

### Benefits

- **Testability:** Inject mock clients for unit tests
- **Flexibility:** Swap implementations (e.g., dry-run mode)
- **Clarity:** Dependencies are explicit, not hidden in global state
