#!/bin/sh
# Pre-push hook implementation: run tests.
set -eu

echo "Running pre-push checks..."
go test ./...
echo "Pre-push checks passed."
