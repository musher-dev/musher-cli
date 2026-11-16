package buildkit

import (
	"context"
	"errors"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

func TestClassifySolveError(t *testing.T) {
	t.Parallel()

	cases := map[string]core.Stage{
		"failed to solve: process \"/bin/sh -c make\" did not complete successfully: exit code: 2": core.StageBuild,
		"failed to solve: dockerfile parse error on line 3: unknown instruction: FORM":             core.StageRecipe,
		"failed to push registry.musher.test/org-1/src-42:c0ffee: 401 Unauthorized":                core.StagePush,
		"server message: insufficient_scope: authorization failed":                                 core.StagePush,
		"rpc error: code = Unavailable desc = connection error: transport is closing":              core.StageInfrastructure,
	}

	for msg, want := range cases {
		if got := classifySolveError(t.Context(), errors.New(msg)); got != want {
			t.Errorf("%q = %s, want %s", msg, got, want)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if got := classifySolveError(ctx, errors.New("failed to solve: anything")); got != core.StageInfrastructure {
		t.Errorf("a canceled build = %s, want infrastructure", got)
	}
}
