# shellcheck shell=bash
# Shell aliases and functions for the Musher dev container.
# This file is meant to be sourced, not executed directly.
#
# Convention:
#   *.shared.sh  — Team defaults (tracked in git)
#   *.local.sh   — Personal overrides (gitignored)

# --- Docker Compose shortcuts ---
alias dc="docker compose -f /workspaces/\${PWD##*/}/.devcontainer/compose.yaml"
alias dcu="dc up -d"
alias dcd="dc down"
alias dcl="dc logs -f"
alias dcp="dc ps"

# --- Git shortcuts ---
alias gs="git status"
alias gl="git log --oneline -20"
alias gd="git diff"

# --- Project shortcuts ---
alias t="task"
