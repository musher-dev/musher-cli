#!/bin/sh
# Install script for Musher CLI
# Usage: curl -fsSL https://get.musher.dev | sh
#
# Options:
#   --version VERSION   Install a specific version (default: latest)
#   --prefix DIR        Install to DIR/bin (default: ~/.local)
#   --yes, -y           Skip confirmation prompts
#   --help, -h          Show this help message

set -eu

REPO="musher-dev/musher-cli"
BINARY="musher"
DEFAULT_PREFIX="$HOME/.local"
BASE_URL="https://github.com/${REPO}"

# ── Helpers ──────────────────────────────────────────────────────────────────

say() {
  printf '%s\n' "$@"
}

err() {
  say "Error: $*" >&2
  exit 1
}

bold() {
  if [ -t 1 ]; then
    printf '\033[1m%s\033[0m\n' "$*"
  else
    say "$*"
  fi
}

green() {
  if [ -t 1 ]; then
    printf '\033[32m%s\033[0m\n' "$*"
  else
    say "$*"
  fi
}

yellow() {
  if [ -t 1 ]; then
    printf '\033[33m%s\033[0m\n' "$*"
  else
    say "$*"
  fi
}

# ── Argument parsing ─────────────────────────────────────────────────────────

VERSION=""
PREFIX=""
YES=false

usage() {
  cat <<EOF
Install Musher CLI

Usage:
  install.sh [options]

Options:
  --version VERSION   Install a specific version (default: latest)
  --prefix DIR        Install to DIR/bin (default: ~/.local)
  --yes, -y           Skip confirmation prompts
  --help, -h          Show this help message

Examples:
  curl -fsSL https://get.musher.dev | sh
  curl -fsSL https://get.musher.dev | sh -s -- --version 1.0.0
  curl -fsSL https://get.musher.dev | sh -s -- --prefix /usr/local
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      [ $# -ge 2 ] || err "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --prefix)
      [ $# -ge 2 ] || err "--prefix requires a value"
      PREFIX="$2"
      shift 2
      ;;
    --yes | -y)
      YES=true
      shift
      ;;
    --help | -h)
      usage
      exit 0
      ;;
    *)
      err "Unknown option: $1"
      ;;
  esac
done

# ── Platform detection ───────────────────────────────────────────────────────

detect_os() {
  os="$(uname -s)"
  case "$os" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    *) err "Unsupported operating system: $os" ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64 | amd64) echo "amd64" ;;
    aarch64 | arm64) echo "arm64" ;;
    *) err "Unsupported architecture: $arch" ;;
  esac
}

# ── Download helpers ─────────────────────────────────────────────────────────

has_curl() {
  command -v curl >/dev/null 2>&1
}

has_wget() {
  command -v wget >/dev/null 2>&1
}

download() {
  url="$1"
  output="$2"

  if has_curl; then
    curl --proto '=https' --tlsv1.2 -fsSL -o "$output" "$url"
  elif has_wget; then
    wget --https-only -q -O "$output" "$url"
  else
    err "Neither curl nor wget found. Please install one and try again."
  fi
}

# ── Version resolution ───────────────────────────────────────────────────────

resolve_latest_version() {
  if has_curl; then
    url=$(curl --proto '=https' --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' \
      "${BASE_URL}/releases/latest" 2>/dev/null) ||
      err "Failed to resolve latest version. Check ${BASE_URL}/releases"
  elif has_wget; then
    url=$(wget --https-only --max-redirect=0 -S \
      "${BASE_URL}/releases/latest" 2>&1 |
      sed -n 's/.*Location: *//p' | tr -d '\r') || true
    [ -n "$url" ] || err "Failed to resolve latest version."
  else
    err "Neither curl nor wget found."
  fi

  tag="${url##*/}"
  [ -n "$tag" ] || err "Could not parse version from redirect URL: $url"
  echo "$tag"
}

# ── Checksum verification ───────────────────────────────────────────────────

verify_checksum() {
  file="$1"
  checksums_file="$2"
  archive_name="$3"

  expected=$(awk -v name="$archive_name" '$2 == name { print $1; exit }' "$checksums_file")
  [ -n "$expected" ] || err "Archive '$archive_name' not found in checksums file"

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$file" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$file" | awk '{print $1}')
  else
    err "Neither sha256sum nor shasum found. Cannot verify checksum."
  fi

  if [ "$actual" != "$expected" ]; then
    err "Checksum mismatch!
  Expected: $expected
  Actual:   $actual"
  fi
}

# ── Sudo helpers ─────────────────────────────────────────────────────────────

maybe_sudo() {
  target="$1"
  dir="$target"
  while [ ! -d "$dir" ]; do
    parent="$(dirname "$dir")"
    if [ "$parent" = "$dir" ]; then
      break
    fi
    dir="$parent"
  done

  if [ -w "$dir" ]; then
    return
  fi

  if command -v sudo >/dev/null 2>&1; then
    echo "sudo"
  else
    err "Cannot write to $target and sudo is not available. Try: --prefix ~/.local"
  fi
}

# ── PATH check ───────────────────────────────────────────────────────────────

check_path() {
  bin_dir="$1"

  case ":${PATH}:" in
    *":${bin_dir}:"*) return 0 ;;
  esac

  yellow "Warning: $bin_dir is not in your PATH."
  say ""
  say "Add it to your shell profile:"

  shell_name="$(basename "${SHELL:-/bin/sh}")"
  case "$shell_name" in
    bash)
      say "  echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.bashrc"
      say "  source ~/.bashrc"
      ;;
    zsh)
      say "  echo 'export PATH=\"$bin_dir:\$PATH\"' >> ~/.zshrc"
      say "  source ~/.zshrc"
      ;;
    fish)
      say "  fish_add_path $bin_dir"
      ;;
    *)
      say "  export PATH=\"$bin_dir:\$PATH\""
      ;;
  esac
}

# ── Main ─────────────────────────────────────────────────────────────────────

main() {
  OS="$(detect_os)"
  ARCH="$(detect_arch)"
  PREFIX="${PREFIX:-$DEFAULT_PREFIX}"
  BIN_DIR="${PREFIX}/bin"

  bold "Musher CLI Installer"
  say ""

  if [ -n "$VERSION" ]; then
    VERSION="${VERSION#v}"
    TAG="v${VERSION}"
  else
    say "Resolving latest version..."
    TAG="$(resolve_latest_version)"
    VERSION="${TAG#v}"
  fi

  ARCHIVE_NAME="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
  ARCHIVE_URL="${BASE_URL}/releases/download/${TAG}/${ARCHIVE_NAME}"
  CHECKSUMS_URL="${BASE_URL}/releases/download/${TAG}/checksums.txt"

  say "  Version:  ${TAG}"
  say "  Platform: ${OS}/${ARCH}"
  say "  Target:   ${BIN_DIR}/${BINARY}"
  say ""

  if [ "$YES" = false ] && [ -t 0 ]; then
    printf "Proceed with installation? [Y/n] "
    read -r reply
    case "$reply" in
      [nN]*)
        say "Aborted."
        exit 0
        ;;
    esac
  fi

  TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t musher)"
  trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

  say "Downloading ${ARCHIVE_NAME}..."
  download "$ARCHIVE_URL" "${TMP_DIR}/${ARCHIVE_NAME}"

  say "Downloading checksums..."
  download "$CHECKSUMS_URL" "${TMP_DIR}/checksums.txt"

  say "Verifying checksum..."
  verify_checksum "${TMP_DIR}/${ARCHIVE_NAME}" "${TMP_DIR}/checksums.txt" "$ARCHIVE_NAME"

  say "Extracting..."
  tar -xzf "${TMP_DIR}/${ARCHIVE_NAME}" -C "$TMP_DIR"

  SUDO="$(maybe_sudo "$BIN_DIR")"
  mkdir -p "$BIN_DIR" 2>/dev/null || ${SUDO} mkdir -p "$BIN_DIR"
  ${SUDO} install -m 755 "${TMP_DIR}/${BINARY}" "${BIN_DIR}/${BINARY}"

  say ""
  green "Successfully installed musher ${TAG} to ${BIN_DIR}/${BINARY}"
  say ""

  check_path "$BIN_DIR"

  say "Get started:"
  say "  musher --help"
  say "  musher login"
}

main
