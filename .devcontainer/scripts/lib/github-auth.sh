#!/usr/bin/env bash
# github-auth.sh — Library: give the container a GitHub credential for gh and git.
#
# Canonical copy: musher-dev/development-container
# .devcontainer/scripts/lib/github-auth.sh. Repositories copy it verbatim and
# change it here first. Sourced, never executed; nothing in it fails its caller.
#
# Two entry points, because Dev Containers sets up git in two steps:
#   github_auth_seed    post-create. Stores a token in gh and creates no git
#                       config: VS Code copies the host's ~/.gitconfig only
#                       after post-create, and only when none exists, so a
#                       file written here costs the developer their identity.
#   ensure_github_auth  post-attach. Seeds if still needed, then points git's
#                       github.com credential at the container's own gh.
#
# Exposes:
#   github_auth_seed, github_auth_wire, ensure_github_auth

if [[ -z "${_MUSHER_GITHUB_AUTH_LOADED:-}" ]]; then
_MUSHER_GITHUB_AUTH_LOADED=1

_GH_AUTH_HOST="github.com"

# The host's ~/.gitconfig reaches the container with these set for the host's
# own gh, naming a binary that is not here (`!/opt/homebrew/bin/gh auth
# git-credential`): gh then reports green while every push 403s.
_GH_AUTH_HELPER_KEYS=(
  "credential.https://github.com.helper"
  "credential.https://gist.github.com.helper"
)

_gh_log() {
  printf 'github-auth: %s\n' "$*" >&2
}

_gh_has() {
  command -v "$1" >/dev/null 2>&1
}

# An env token outranks gh's stored login, and `gh auth login` refuses to
# manage one. Codespaces injects GITHUB_TOKEN, so such a container is answered.
_gh_env_token() {
  [[ -n "${GH_TOKEN:-}${GITHUB_TOKEN:-}" ]]
}

# `gh auth token` reads the stored credential locally, so an offline start is
# not mistaken for a missing login, as `gh auth status` would.
_gh_has_credential() {
  gh auth token --hostname "${_GH_AUTH_HOST}" >/dev/null 2>&1
}

# Captured rather than piped into `grep -q`: its early exit SIGPIPEs the writer,
# which callers running under `set -o pipefail` read as a failure.
_gh_git_wired() {
  local gh_bin helpers
  gh_bin="$(command -v gh)" || return 1
  helpers="$(git config --global --get-all "${_GH_AUTH_HELPER_KEYS[0]}" 2>/dev/null)" || return 1
  grep -qxF "!${gh_bin} auth git-credential" <<<"${helpers}"
}

# Removes the host-copied helpers, and a previous `gh auth setup-git`, before
# git is asked for a credential. VS Code's own `credential.helper` stays: it is
# the channel the seeding rides on. Writes only to a ~/.gitconfig that exists.
_gh_clear_url_scoped_helpers() {
  local key
  for key in "${_GH_AUTH_HELPER_KEYS[@]}"; do
    git config --global --get-all "${key}" >/dev/null 2>&1 || continue
    if git config --global --unset-all "${key}" 2>/dev/null; then
      _gh_log "cleared ${key#credential.} inherited from the host."
    else
      _gh_log "WARNING: could not clear ${key#credential.}"
    fi
  done
}

# Asks git for a credential, which VS Code's forwarded helper answers from the
# host's own store: Keychain, Credential Manager, libsecret or gh. Both prompt
# switches matter, because the editor exports GIT_ASKPASS, which outranks
# GIT_TERMINAL_PROMPT=0, and a prompt would hang the lifecycle command.
_gh_token_from_host() {
  local filled
  filled="$(printf 'protocol=https\nhost=%s\n\n' "${_GH_AUTH_HOST}" \
    | env -u GIT_ASKPASS -u SSH_ASKPASS GIT_TERMINAL_PROMPT=0 \
      timeout 20 git -C "${HOME}" credential fill 2>/dev/null)" || return 1
  awk '/^password=/ { print substr($0, 10); exit }' <<<"${filled}"
}

# GitHub refuses a token without the repo and read:org scopes. stderr is dropped
# because it is the one place a credential could be echoed back.
_gh_seed_from() {
  local token="$1" origin="$2"
  [[ -n "${token}" ]] || return 1
  if printf '%s' "${token}" | gh auth login --hostname "${_GH_AUTH_HOST}" --with-token 2>/dev/null; then
    _gh_log "seeded gh from ${origin}."
    return 0
  fi
  _gh_log "GitHub refused the credential from ${origin} (gh needs repo and read:org); ignoring it."
  return 1
}

# Stores a GitHub token in gh when none is there: the host's exported GH_TOKEN
# first, then the host's git credential. The second exists only once an editor
# has attached and has written ~/.gitconfig, and asking it can open a sign-in
# window on the host, so it is tried last.
#
# Globals:
#   MUSHER_HOST_GH_TOKEN — read, devcontainer.json passes ${localEnv:GH_TOKEN}
# Outputs:
#   Writes progress to stderr
# Returns:
#   0 always
github_auth_seed() {
  _gh_has gh || { _gh_log "gh is not installed; skipping."; return 0; }
  if _gh_env_token; then
    _gh_log "GH_TOKEN/GITHUB_TOKEN is set; gh reads it directly."
    return 0
  fi
  _gh_has_credential && return 0

  _gh_seed_from "${MUSHER_HOST_GH_TOKEN:-}" "the host's exported GH_TOKEN" && return 0
  if [[ -f "${HOME}/.gitconfig" ]] && _gh_has git; then
    _gh_clear_url_scoped_helpers
    local host_token
    host_token="$(_gh_token_from_host || true)"
    _gh_seed_from "${host_token}" "the host's git credential helper" || true
    unset host_token
  fi

  _gh_has_credential && return 0
  _gh_log "no GitHub credential reached the container yet. The next editor attach tries again; or run 'gh auth login'."
  return 0
}

# Points git's github.com credential at the container's gh. Only once VS Code
# has written ~/.gitconfig, and only once gh holds a token: `gh auth setup-git`
# starts its section with an empty `helper =`, which also drops the forwarded
# helper for github.com.
#
# Outputs:
#   Writes progress to stderr
# Returns:
#   0 always
github_auth_wire() {
  if ! _gh_has gh || ! _gh_has git; then
    return 0
  fi
  _gh_env_token && return 0
  [[ -f "${HOME}/.gitconfig" ]] || return 0
  _gh_has_credential || return 0
  _gh_git_wired && return 0

  _gh_clear_url_scoped_helpers
  if gh auth setup-git --hostname "${_GH_AUTH_HOST}" 2>/dev/null; then
    _gh_log "git now resolves ${_GH_AUTH_HOST} through this container's gh."
  else
    _gh_log "WARNING: 'gh auth setup-git' failed; pushes may 403. Run it by hand."
  fi
  return 0
}

# Seeds gh if needed, then wires git to it. Run from post-attach.
#
# Returns:
#   0 always
ensure_github_auth() {
  github_auth_seed
  github_auth_wire
  return 0
}

fi
