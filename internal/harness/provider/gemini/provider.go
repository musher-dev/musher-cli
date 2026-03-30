// Package gemini provides the harness provider for Google Gemini CLI.
package gemini

import (
	_ "embed"

	"github.com/musher-dev/musher-cli/internal/harness"
)

//go:embed spec.yaml
var specData []byte

// Module is the harness module for Google Gemini CLI registration.
var Module = &harness.Module{Spec: harness.MustParseSpec(specData)}
