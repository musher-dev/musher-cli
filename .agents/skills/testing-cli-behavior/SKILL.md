---
name: testing-cli-behavior
description: >-
  Design integration testing strategies for CLI applications including
  script-based behavior tests, exit code verification, stdout/stderr
  assertions, file system mutation testing, isolated test environments,
  golden file patterns, and the test pyramid for CLIs (unit, integration,
  end-to-end). Use when designing CLI test suites, writing integration tests
  for command-line tools, testing interactive prompts, or establishing CLI
  test patterns.
  Triggered by: CLI testing, integration test CLI, test exit codes, test
  stdout, test stderr, golden file, CLI test strategy, testscript, behavior
  test, command-line testing, test pyramid CLI.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# CLI Behavior Testing

Guidance for testing command-line applications at every level — from unit tests of business logic to end-to-end verification of the compiled binary.

## Purpose

Testing a CLI differs fundamentally from testing a library. Unit tests for internal functions are necessary but insufficient. You must verify the actual user interaction: does the binary produce the right exit code? Does the right content appear on stdout? Does stderr contain a useful error message? Are the right files created on disk?

This skill provides patterns for building a comprehensive CLI test suite that catches regressions at the right level of abstraction.

---

## CLI Test Pyramid

| Level | What It Tests | Speed | Isolation | Catches |
|-------|--------------|-------|-----------|---------|
| **Unit** | Service functions, validators, parsers | Fast | Full (mocks) | Logic bugs, edge cases |
| **Integration** | Command execution with real services | Medium | Partial (test DB, mock API) | Wiring bugs, config errors |
| **End-to-End** | Compiled binary in a subprocess | Slow | None (real environment) | Packaging, flag parsing, exit codes |

### Distribution Rule

Aim for approximately:
- 60% unit tests (fast, focused on business logic)
- 30% integration tests (command execution, service wiring)
- 10% end-to-end tests (compiled binary, critical user flows)

---

## Script-Based Behavior Tests

### Concept

Write tests as text files that simulate a shell session. Each file describes a scenario with commands to run and assertions about stdout, stderr, and exit codes.

### Format

```
# Scenario description as a comment

# Execute a command (expect success)
exec mycli project list
stdout 'NAME'
stdout 'my-project'
! stderr .

# Execute a command (expect failure)
! exec mycli project get nonexistent
stderr 'not found'
! stdout .
```

### Assertion Types

| Assertion | Meaning |
|-----------|---------|
| `stdout 'pattern'` | stdout contains a line matching the pattern |
| `stderr 'pattern'` | stderr contains a line matching the pattern |
| `! stdout .` | stdout is empty |
| `! stderr .` | stderr is empty |
| `! exec cmd` | Command exits with non-zero status |
| `exec cmd` | Command exits with zero status |
| `exists file` | File exists after command runs |
| `! exists file` | File does not exist |
| `cmp file expected` | File contents match expected file |

### Environment Setup

Each test should run in an isolated temporary directory:

```
# Set up environment
env MYCLI_API_TOKEN=test-token
env MYCLI_PROFILE=local

# Create fixtures
mkdir .mycli
cp fixture-config.yaml .mycli/config.yaml

# Run the test
exec mycli config show
stdout 'api-url: http://localhost:8000'
```

### Benefits Over Bash Test Scripts

| Concern | Bash Scripts | Script-Based Testing |
|---------|-------------|---------------------|
| Cross-platform | Fragile (shell differences) | Consistent (emulated environment) |
| Isolation | Manual cleanup required | Automatic temp directory per test |
| Assertions | String comparison hacks | Built-in pattern matching |
| Readability | Obscured by boilerplate | Declarative and concise |
| Integration | Separate runner | Runs inside standard test framework |

---

## Golden File Testing

### Concept

Compare command output against a saved "golden" file. If the output changes, the test fails — forcing a deliberate review of whether the change is intentional.

### Workflow

1. Run the command and capture output
2. Compare against the golden file in `testdata/`
3. If different: fail the test and show the diff
4. To update: run with an update flag (e.g., `--update-golden`)

### When to Use Golden Files

| Scenario | Golden File? | Why |
|----------|-------------|-----|
| Help text output | Yes | Catches unintended help text changes |
| Formatted table output | Yes | Catches column alignment regressions |
| JSON schema output | Yes | Catches field name or structure changes |
| Error messages | No (use pattern matching) | Error messages may include dynamic content |
| Version output | No | Changes with every release |

### Golden File Organization

```
testdata/
├── golden/
│   ├── project-list-table.txt
│   ├── project-list-json.txt
│   ├── help-root.txt
│   └── help-project-create.txt
└── fixtures/
    ├── config-staging.yaml
    └── sample-project.json
```

---

## Exit Code Testing

### Standard Exit Codes to Test

| Scenario | Expected Code | Test |
|----------|--------------|------|
| Successful operation | 0 | `exec mycli project list` |
| General error | 1 | `! exec mycli project get invalid` |
| Usage error (bad flags) | 2 | `! exec mycli --unknown-flag` |
| Auth failure | 3 | `! exec mycli project list` (no token) |
| Not found | varies | `! exec mycli project get nonexistent` |

### Testing Exit Codes Programmatically

```
# Run the binary as a subprocess
result = run_subprocess(["mycli", "project", "get", "nonexistent"])

assert result.exit_code == 1
assert "not found" in result.stderr
assert result.stdout == ""
```

---

## Testing Interactive Prompts

Interactive prompts (TUI, y/n confirmations) are harder to test. Strategies:

### Strategy 1: Bypass Flags

Provide flags that bypass interactive prompts:

```
# Interactive
mycli project delete my-project
# > Are you sure? (y/n)

# Non-interactive (testable)
mycli project delete my-project --yes
mycli project delete my-project --force
```

**Always test the non-interactive path.** Interactive testing is supplementary.

### Strategy 2: Stdin Piping

Pipe input to simulate user responses:

```
echo "y" | mycli project delete my-project
```

### Strategy 3: Environment Detection

If `CI=true` or `isatty(stdin) == false`, skip interactive prompts and fail with an error message indicating that the `--yes` flag is required.

---

## Test Fixtures and Factories

### Config File Fixtures

Create test config files that exercise different scenarios:

```
testdata/fixtures/
├── config-valid.yaml          # Standard valid config
├── config-missing-url.yaml    # Missing required field
├── config-invalid-url.yaml    # Malformed URL value
├── config-empty-profiles.yaml # No profiles defined
└── config-multi-profile.yaml  # Multiple profiles for switching tests
```

### Mock API Server

For integration tests that hit an API:

1. Start a lightweight HTTP server in the test setup
2. Configure it to return canned responses
3. Point the CLI at `http://localhost:<test-port>`
4. Assert the CLI produced correct output from the canned data

### Test Environment Variables

Set environment variables in test setup to control behavior:

```
env MYCLI_API_URL=http://localhost:9999
env MYCLI_API_TOKEN=test-token-abc
env MYCLI_PROFILE=test
env NO_COLOR=1
```

---

## CI-Specific Testing Considerations

| Concern | Approach |
|---------|----------|
| No interactive terminal | Run with `CI=true`, test `--yes` flags |
| No keyring available | Use env var auth, test headless fallback |
| Cross-platform | Test on Linux, macOS, Windows runners |
| Binary compilation | Build before test, test the compiled binary |
| Test parallelism | Each test gets isolated temp directory |
| Flaky network tests | Mock API server, don't hit real endpoints |
