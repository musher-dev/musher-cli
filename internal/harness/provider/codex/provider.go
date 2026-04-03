// Package codex provides the harness provider for OpenAI Codex.
package codex

import (
	_ "embed"

	"github.com/musher-dev/musher-cli/internal/harness"
)

//go:embed spec.yaml
var specData []byte

// Module is the harness module for OpenAI Codex registration.
var Module = &harness.Module{
	Spec:           harness.MustParseSpec(specData),
	AgentTransform: harness.TransformAgentToTOML,
	AgentFileExt:   ".toml",
}
