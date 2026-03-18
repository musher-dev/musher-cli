# Shell Customization

Files in this directory are sourced by zsh on shell startup using the shared/local pattern.

## Convention

| Pattern | Tracked | Purpose |
|---|---|---|
| `*.shared.sh` | Yes | Team defaults — aliases, functions, plugin config |
| `*.local.sh` | No (gitignored) | Personal overrides — machine-specific settings |

Shared files are sourced first, then local files, so local settings override team defaults.

## Usage

- Edit `aliases.shared.sh` to change team-wide aliases
- Create a `*.local.sh` file for personal customizations (e.g., `my.local.sh`)
- Open a new terminal — changes are picked up automatically

## Notes

- Files are sourced in alphabetical order within each group
- `*.local.sh` files are gitignored and will not be committed
