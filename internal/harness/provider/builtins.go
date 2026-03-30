package provider

import (
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/provider/claude"
	"github.com/musher-dev/musher-cli/internal/harness/provider/codex"
	"github.com/musher-dev/musher-cli/internal/harness/provider/copilot"
	"github.com/musher-dev/musher-cli/internal/harness/provider/cursor"
	"github.com/musher-dev/musher-cli/internal/harness/provider/gemini"
	"github.com/musher-dev/musher-cli/internal/harness/provider/opencode"
)

// NewBuiltinRegistry creates a Registry populated with all built-in providers.
func NewBuiltinRegistry() *harness.Registry {
	reg := harness.NewRegistry()
	harness.RegisterBuiltins(reg,
		claude.Module,
		codex.Module,
		copilot.Module,
		cursor.Module,
		gemini.Module,
		opencode.Module,
	)

	return reg
}
