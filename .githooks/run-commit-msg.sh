#!/bin/sh
# Commit-msg hook implementation: enforce Conventional Commits.
set -eu

COMMIT_MSG_FILE="${1:?COMMIT_MSG_FILE is required}"

# Read first line of commit message
FIRST_LINE=$(head -n 1 "$COMMIT_MSG_FILE")

# Skip merge commits
case "$FIRST_LINE" in
  Merge*) exit 0 ;;
esac

# Validate Conventional Commits format
PATTERN='^(feat|fix|chore|docs|style|refactor|perf|test|ci|build|revert)(\(.+\))?(!)?: .+'
if ! echo "$FIRST_LINE" | grep -qE "$PATTERN"; then
  echo "ERROR: Commit message does not follow Conventional Commits format."
  echo ""
  echo "  Expected: <type>(<scope>): <description>"
  echo "  Got:      $FIRST_LINE"
  echo ""
  echo "  Types: feat, fix, chore, docs, style, refactor, perf, test, ci, build, revert"
  echo ""
  exit 1
fi
