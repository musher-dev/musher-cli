// Package copilot provides the harness provider for GitHub Copilot.
package copilot

import (
	_ "embed"

	"github.com/musher-dev/musher-cli/internal/harness"
)

//go:embed spec.yaml
var specData []byte

// Module is the harness module for GitHub Copilot registration.
var Module = &harness.Module{Spec: harness.MustParseSpec(specData)}
