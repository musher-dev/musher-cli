---
name: securing-cli-credentials
description: >-
  Design secure credential management for CLI applications including OS keyring
  integration, environment variable fallback for headless/CI contexts, token
  provider abstraction, auth middleware injection for API clients, and
  plaintext storage prohibition. Use when implementing CLI authentication,
  designing token storage, adding keyring support, or reviewing credential
  security in a CLI tool.
  Triggered by: CLI credentials, keyring, token storage, CLI authentication,
  secure storage, credential management, API token, CLI auth, headless auth,
  CI authentication, token provider.
allowed-tools: Read, Grep, Glob, Edit, Write
---

# CLI Credential Security

Guidance for securely storing and retrieving authentication tokens in CLI applications across interactive and headless environments.

## Purpose

Authentication tokens are the most sensitive data a CLI handles. A single leaked token can compromise an entire platform. Yet many CLIs store tokens in plaintext config files — files that get checked into dotfile repos, shared in screen recordings, or leaked through backup tools.

This skill provides patterns for secure credential storage that work for both interactive human users (OS keyring) and automated pipelines (environment variables), with a clear abstraction layer that keeps the rest of the application unaware of the storage mechanism.

---

## Storage Method Comparison

| Method | Security | Convenience | Context |
|--------|----------|-------------|---------|
| Plaintext config file | Low | High | **Prohibited.** High risk of accidental exposure |
| Environment variable | Medium | Medium | Best for CI/CD and automation scripts |
| OS keyring | High | High | Best for interactive human use |
| Encrypted file | Medium-High | Low | Fallback when keyring is unavailable |

### Decision Rule

```
if (interactive terminal AND keyring available):
    use OS keyring
else if (CI environment OR headless server):
    use environment variable
else:
    use encrypted file with user-provided passphrase (last resort)

NEVER: store in plaintext config file
```

---

## OS Keyring Integration

### Platform Coverage

| Platform | Backend | Library/API |
|----------|---------|-------------|
| macOS | Keychain Access | Security framework, `security` CLI |
| Windows | Windows Credential Manager | WinCred API |
| Linux (desktop) | Secret Service API (GNOME Keyring, KWallet) | libsecret, D-Bus |
| Linux (headless) | Not available | Fall back to env var or encrypted file |

### Keyring Operations

| Operation | Purpose |
|-----------|---------|
| `store(service, account, token)` | Save a credential |
| `retrieve(service, account)` | Get a credential |
| `delete(service, account)` | Remove a credential |

### Naming Convention

Use consistent, namespaced identifiers for keyring entries:

- **Service name:** The CLI binary name (e.g., `mycli`)
- **Account name:** The profile or context (e.g., `production`, `staging`)

This allows multiple credentials for different environments to coexist.

---

## Token Provider Abstraction

Decouple credential retrieval from the code that uses credentials. The application should never know where the token came from.

### Interface Pattern

```
interface TokenProvider:
    method get_token() -> (token: string, error)
```

### Implementations

```
KeyringTokenProvider:
    - Reads from OS keyring
    - Falls back to env var if keyring unavailable

EnvironmentTokenProvider:
    - Reads from a specific environment variable (e.g., MYCLI_API_TOKEN)

StaticTokenProvider:
    - Returns a hardcoded token (for testing only)

ChainedTokenProvider:
    - Tries providers in order: keyring -> env var -> prompt user
```

### Resolution Chain

The default token provider should attempt sources in this order:

1. **Explicit flag:** `--token` or `--api-key` (highest priority)
2. **Environment variable:** `MYCLI_API_TOKEN`
3. **OS keyring:** Lookup by service + active profile
4. **Interactive prompt:** Ask the user (only if stdout is a terminal)
5. **Fail with clear message:** Indicate which sources were attempted

---

## Auth Middleware Injection

Generated API clients should not handle authentication directly. Instead, inject auth as middleware that intercepts requests before they are sent.

### Pattern

```
function create_authenticated_client(base_url, token_provider):
    auth_middleware = function(request):
        token = token_provider.get_token()
        request.headers["Authorization"] = "Bearer " + token
        return request

    return create_client(base_url, middleware=[auth_middleware])
```

### Benefits

- **Separation of concerns:** Generated client code stays untouched
- **Testability:** Inject a static token provider in tests
- **Flexibility:** Swap token providers without changing client code
- **Regeneration safety:** Re-generating the API client never overwrites auth logic

---

## Login / Logout Flow

### Login Command

```
mycli auth login
```

1. Check if a valid token already exists (keyring or env)
2. If no token: prompt for API key or initiate OAuth flow
3. Validate the token by making a lightweight API call (e.g., `GET /me`)
4. Store the validated token in the keyring under the active profile
5. Print confirmation: "Authenticated as <username> (profile: staging)"

### Logout Command

```
mycli auth logout
```

1. Remove the token from the keyring for the active profile
2. Print confirmation: "Logged out from profile: staging"

### Status Command

```
mycli auth status
```

1. Check for a valid token in the resolution chain
2. If found: validate with a lightweight API call
3. Print: source of the token, associated user, expiration (if applicable)
4. If not found: print which sources were checked and that none had a valid token

---

## Headless / CI Detection

### Detection Signals

| Signal | Interpretation |
|--------|---------------|
| `CI=true` environment variable | Running in CI (GitHub Actions, GitLab CI, etc.) |
| `TERM=dumb` or unset | No interactive terminal |
| `isatty(stdout) == false` | Output is piped or redirected |
| Keyring backend returns "not available" | No desktop environment |

### Headless Behavior

When headless is detected:

1. **Skip keyring access** (avoid error messages about missing D-Bus)
2. **Require environment variable** for authentication
3. **Never prompt for input** (would hang the pipeline)
4. **Exit with clear error** if no token is found: "Error: MYCLI_API_TOKEN environment variable is required in non-interactive mode"

---

## Security Checklist

- [ ] Tokens are never written to config files, logs, or stdout
- [ ] Keyring is the default storage for interactive use
- [ ] Environment variable is the documented approach for CI/CD
- [ ] Token is validated on login (not just stored blindly)
- [ ] Failed auth produces a clear error, not a stack trace
- [ ] `--token` flag value is not logged or printed in verbose mode
- [ ] Token provider is injected, not hardcoded
- [ ] Logout removes tokens from all storage backends
- [ ] `auth status` shows where the token is coming from
