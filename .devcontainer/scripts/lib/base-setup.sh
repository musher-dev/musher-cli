#!/usr/bin/env bash
# base-setup.sh — Reusable setup orchestrator for musher dev containers.
#
# This file is intended to be sourced, not executed directly.
# Source it and call base_setup, or call individual functions to customize.
#
# Usage:
#   source "path/to/base-setup.sh"
#   base_setup
set -euo pipefail

# Guard against direct execution — this file must be sourced.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "Error: source this file, don't execute it" >&2
  exit 1
fi

_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly _LIB_DIR
_HOME="/home/${REMOTE_USER:-vscode}"
readonly _HOME

# shellcheck source=.devcontainer/scripts/lib/common.sh
source "${_LIB_DIR}/common.sh"

# --- Config directories ---

# Creates standard config directories for dev tools.
#
# Globals:
#   _HOME — read, user home directory
# Outputs:
#   Writes progress to stderr via log()
base_setup_config_dirs() {
  setup_config_dirs \
    "gh config:${_HOME}/.config/gh" \
    "claude:${_HOME}/.claude" \
    "codex:${_HOME}/.codex" \
    "shell history:${_HOME}/.shell-history"
}

# --- Cache directories ---

# Creates cache directories for all dev tools so caches land
# under a single tree instead of scattering across the filesystem.
#
# Globals:
#   _HOME — read, user home directory
# Outputs:
#   Writes progress to stderr via log()
base_setup_cache_dirs() {
  setup_config_dirs \
    "xdg cache:${_HOME}/.cache" \
    "uv cache:${_HOME}/.cache/uv" \
    "ruff cache:${_HOME}/.cache/ruff" \
    "pip cache:${_HOME}/.cache/pip" \
    "mypy cache:${_HOME}/.cache/mypy" \
    "npm cache:${_HOME}/.cache/npm" \
    "deno cache:${_HOME}/.cache/deno" \
    "go mod cache:${_HOME}/.cache/go/mod" \
    "go build cache:${_HOME}/.cache/go/build" \
    "bun cache:${_HOME}/.cache/bun"
}

# --- GitHub ---

# Stores a GitHub token in gh when one has reached the container, and writes no
# git config: VS Code copies the host's ~/.gitconfig only after post-create.
# post-attach.sh finishes the job (lib/github-auth.sh). After the config and
# cache dirs, because gh writes to both.
#
# Outputs:
#   Writes progress to stderr
base_setup_github_seed() {
  # shellcheck source=.devcontainer/scripts/lib/github-auth.sh
  source "${_LIB_DIR}/github-auth.sh"
  github_auth_seed
}

# --- Shell history ---

# Points zsh and bash at the shell-history volume, so history survives a
# rebuild. The line goes after what each rc file already holds, because
# VS Code's shell integration sets HISTFILE before it sources ~/.zshrc and
# oh-my-zsh keeps a value already set. Not exported, so a child shell keeps
# its own file.
#
# Globals:
#   _HOME — read, user home directory
# Outputs:
#   Writes progress to stderr via log()
base_setup_shell_history() {
  local marker="# musher devcontainer history (post-create)"
  local rc line
  [[ -d "${_HOME}/.shell-history" ]] || return 0
  for rc in "${_HOME}/.zshrc" "${_HOME}/.bashrc"; do
    [[ -f "${rc}" ]] || continue
    grep -qxF "${marker}" "${rc}" && continue
    # shellcheck disable=SC2016  # expanded by the shell that reads the rc file
    case "${rc}" in
      *zshrc) line='HISTFILE="$HOME/.shell-history/zsh_history"' ;;
      *) line='HISTFILE="$HOME/.shell-history/bash_history"; PROMPT_COMMAND="history -a${PROMPT_COMMAND:+; $PROMPT_COMMAND}"' ;;
    esac
    log "Keeping $(basename "${rc}") history on the shell-history volume..."
    printf '\n%s\n%s\n' "${marker}" "${line}" >> "${rc}"
  done
}

# --- NVM ---

# Delegates to fix_nvm_permissions from common.sh.
base_fix_nvm_permissions() {
  fix_nvm_permissions
}

# --- Claude Code ---

# Installs Claude Code using the native installer if not already present.
#
# Outputs:
#   Writes progress to stderr via log()
# Returns:
#   0 on success, non-zero on failure
base_install_claude_code() {
  if has_cmd claude; then
    log "Claude Code already installed, skipping"
    return 0
  fi
  log "Installing Claude Code via native installer..."
  # pipefail, or a failed download pipes an empty script into bash, which
  # exits 0, so retry would never see the failure.
  retry 3 5 bash -c 'set -o pipefail; curl -fsSL https://claude.ai/install.sh | bash'
}

# --- Codex CLI ---

# Installs the Codex CLI if not already present.
#
# Outputs:
#   Writes progress to stderr via log()
# Returns:
#   0 on success, non-zero on failure
base_install_codex() {
  if has_cmd codex; then
    log "Codex CLI already installed, skipping"
    return 0
  fi
  install_npm_cli "@openai/codex"
}

# --- Verify ---

# Verifies all expected base tools are installed.
#
# Outputs:
#   Writes tool status to stderr via log()
# Returns:
#   0 if all tools found, 1 if any are missing
base_verify_tools() {
  verify_tools gh claude task codex
}

# --- Orchestrator ---

# Runs the complete base setup sequence.
#
# Outputs:
#   Writes progress to stderr via log()
base_setup() {
  log "Running base setup..."
  base_setup_config_dirs
  base_setup_cache_dirs
  base_setup_github_seed
  base_setup_shell_history
  base_fix_nvm_permissions
  base_install_claude_code
  base_install_codex
  base_verify_tools
  log "Base setup complete"
}
