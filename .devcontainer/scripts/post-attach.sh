#!/usr/bin/env bash
# post-attach.sh — DevContainer post-attach command hook.
#
# Runs each time an editor attaches. Only then do VS Code's forwarded
# credential helper and its copy of the host's ~/.gitconfig both exist, so the
# GitHub credential is finished here rather than in post-create. Quick once
# done, and never fails the attach (lib/github-auth.sh).
#
# Usage: Called automatically by devcontainer.json postAttachCommand.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR

# shellcheck source=.devcontainer/scripts/lib/github-auth.sh
source "${SCRIPT_DIR}/lib/github-auth.sh"

# Entry point: seeds gh if still needed, then wires git to it.
#
# Arguments:
#   $@ — passed through (unused, reserved for future use)
main() {
  ensure_github_auth
  # --- Add repo-specific attach steps below ---
}

main "$@"
