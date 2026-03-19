#!/usr/bin/env bash
# motd.sh — Renders a startup MOTD summarizing the dev container state.
#
# This is a library file meant to be sourced, not executed directly.
# Requires common.sh (has_cmd, log) to be sourced first.
#
# Usage: source "path/to/motd.sh"; show_motd "/path/to/compose.yaml"

if [[ -z "${_MOTD_SH_LOADED:-}" ]]; then
  readonly _MOTD_SH_LOADED=1

  # --- Color setup ---

  _motd_setup_colors() {
    if [[ -t 1 ]] && has_cmd tput; then
      _BOLD="$(tput bold)"
      _DIM="$(tput dim)"
      _GREEN="$(tput setaf 2)"
      _YELLOW="$(tput setaf 3)"
      _RED="$(tput setaf 1)"
      _CYAN="$(tput setaf 6)"
      _RESET="$(tput sgr0)"
    else
      _BOLD="" _DIM="" _GREEN="" _YELLOW="" _RED="" _CYAN="" _RESET=""
    fi
  }

  # --- Sub-functions ---

  _motd_header() {
    local line
    line="$(printf '═%.0s' {1..58})"
    echo "${_BOLD}${line}${_RESET}"
    echo "${_BOLD}  Musher Dev Container${_RESET}"
    echo "${_BOLD}${line}${_RESET}"
  }

  # Prints a detected runtime version, or skips if not found.
  #
  # Arguments:
  #   $1 — command name
  #   $2 — display label
  #   $3 — version extraction command (eval'd)
  _motd_runtime_entry() {
    local cmd="$1" label="$2" version_cmd="$3"
    if has_cmd "$cmd"; then
      local ver
      ver="$(eval "$version_cmd" 2> /dev/null || echo "?")"
      printf "  ${_CYAN}%-9s${_RESET} %-14s" "$label" "$ver"
    else
      printf "  %-9s %-14s" "" ""
    fi
  }

  _motd_runtimes() {
    local sep
    sep="$(printf '─%.0s' {1..54})"
    echo ""
    echo "  ${_BOLD}Runtimes${_RESET}"
    echo "  ${_DIM}${sep}${_RESET}"

    # Row 1: node + python
    _motd_runtime_entry node "node" "node -v"
    _motd_runtime_entry python3 "python" "python3 -c 'import platform; print(platform.python_version())'"
    echo ""

    # Row 2: go + java
    _motd_runtime_entry go "go" "go version | grep -oP '\\d+\\.\\d+\\.\\d+'"
    _motd_runtime_entry java "java" "java -version 2>&1 | head -1 | grep -oP '\\d+[\\d.]+'"
    echo ""

    # Row 3: deno + bun
    _motd_runtime_entry deno "deno" "deno -v | head -1 | awk '{print \$2}'"
    _motd_runtime_entry bun "bun" "bun -v"
    echo ""
  }

  _motd_services() {
    local compose_file="$1"
    if [[ -z "$compose_file" ]] || [[ ! -f "$compose_file" ]] || ! has_cmd docker; then
      return 0
    fi

    local output
    output="$(docker compose -f "$compose_file" ps --format json 2> /dev/null || true)"
    if [[ -z "$output" ]]; then
      return 0
    fi

    local sep
    sep="$(printf '─%.0s' {1..54})"
    echo ""
    printf "  ${_BOLD}%-35s %s${_RESET}\n" "Services" "Status"
    echo "  ${_DIM}${sep}${_RESET}"

    echo "$output" | while IFS= read -r line; do
      [[ -z "$line" ]] && continue

      local name state health ports
      name="$(echo "$line" | grep -oP '"Name"\s*:\s*"\K[^"]+' | head -1)"
      state="$(echo "$line" | grep -oP '"State"\s*:\s*"\K[^"]+' | head -1)"
      health="$(echo "$line" | grep -oP '"Health"\s*:\s*"\K[^"]+' | head -1)"

      # Extract published host port
      ports="$(echo "$line" | grep -oP '"PublishedPort"\s*:\s*\K\d+' | head -1)"

      [[ -z "$name" ]] && continue

      # Build display name
      local display_name="$name"
      if [[ -n "$ports" ]] && [[ "$ports" != "0" ]]; then
        display_name="${name} (${ports})"
      fi

      # Determine status label and color
      local status_label color
      if [[ -n "$health" ]] && [[ "$health" != "" ]]; then
        status_label="$health"
      else
        status_label="$state"
      fi

      case "$status_label" in
        healthy) color="${_GREEN}" ;;
        starting) color="${_YELLOW}" ;;
        *) color="${_RED}" ;;
      esac

      printf "  %-35s ${color}%s${_RESET}\n" "$display_name" "$status_label"
    done
  }

  _motd_aliases() {
    local sep
    sep="$(printf '─%.0s' {1..54})"
    echo ""
    echo "  ${_BOLD}Quick Reference${_RESET}"
    echo "  ${_DIM}${sep}${_RESET}"
    echo "  dc / dcu / dcd / dcl             Docker Compose"
    echo "  gs / gl / gd                     Git shortcuts"
    echo "  t                                Task runner"
    echo "  claude                           Claude Code AI"
  }

  _motd_tips() {
    local sep
    sep="$(printf '─%.0s' {1..54})"
    echo ""
    echo "  ${_BOLD}Tips${_RESET}"
    echo "  ${_DIM}${sep}${_RESET}"
    echo "  * Personal shell config:   config/shell/<name>.local.sh"
    echo "  * Enable services:         edit .devcontainer/.env"
    echo "  * Available profiles:      redis, minio, registry,"
    echo "                             azimutt, observability"
    echo "  * Shell config docs:       config/shell/README.md"
  }

  # Renders the full MOTD to stdout.
  #
  # Arguments:
  #   $1 — path to compose.yaml (may be empty to skip services)
  # Outputs:
  #   MOTD text to stdout
  show_motd() {
    local compose_file="${1:-}"
    _motd_setup_colors

    local border
    border="$(printf '═%.0s' {1..58})"

    echo ""
    _motd_header
    _motd_runtimes
    _motd_services "$compose_file"
    _motd_aliases
    _motd_tips
    echo "${_BOLD}${border}${_RESET}"
    echo ""
  }

fi
