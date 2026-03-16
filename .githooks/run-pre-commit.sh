#!/bin/sh
# Pre-commit hook implementation: format + lint staged Go files.
set -eu

# Get list of staged Go files
STAGED_GO_FILES=$(git diff --cached --name-only --diff-filter=ACM -- '*.go' || true)

if [ -z "$STAGED_GO_FILES" ]; then
  exit 0
fi

echo "Running pre-commit checks on staged Go files..."

# Check formatting
if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint fmt --diff $STAGED_GO_FILES
fi

# Run go vet
go vet ./...

echo "Pre-commit checks passed."
