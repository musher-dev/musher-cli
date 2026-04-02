// Package opencode provides the harness provider for OpenCode.
package opencode

import (
	_ "embed"

	"github.com/musher-dev/musher-cli/internal/harness"
)

//go:embed spec.yaml
var specData []byte

// Module is the harness module for OpenCode registration.
var Module = &harness.Module{
	Spec:           harness.MustParseSpec(specData),
	AgentTransform: harness.TransformToolsToRecord,
}
