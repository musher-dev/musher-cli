// Package claude provides the harness provider for Anthropic's Claude Code.
package claude

import (
	_ "embed"

	"github.com/musher-dev/musher-cli/internal/harness"
)

//go:embed spec.yaml
var specData []byte

// Module is the harness module for Claude Code registration.
var Module = &harness.Module{Spec: harness.MustParseSpec(specData)}
