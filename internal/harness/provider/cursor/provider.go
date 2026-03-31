// Package cursor provides the harness provider for Cursor.
package cursor

import (
	_ "embed"

	"github.com/musher-dev/musher-cli/internal/harness"
)

//go:embed spec.yaml
var specData []byte

// Module is the harness module for Cursor registration.
var Module = &harness.Module{Spec: harness.MustParseSpec(specData)}
