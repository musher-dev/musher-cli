#!/bin/bash
# Post-create script for devcontainer setup
set -euo pipefail

echo "Setting up Musher CLI development environment..."

# Install Task
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

# Install Go tools
go install golang.org/x/vuln/cmd/govulncheck@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# Install git hooks
git config core.hooksPath .githooks

# Download dependencies
go mod download

echo "Development environment ready!"
