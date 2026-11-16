package core

import (
	"errors"
	"testing"
)

func TestStages(t *testing.T) {
	t.Parallel()

	base := errors.New("boom")

	if AtStage(StageBuild, nil) != nil {
		t.Fatal("AtStage(nil) is not nil")
	}

	err := AtStage(StagePush, AtStage(StageRecipe, base))
	if StageOf(err) != StageRecipe || !errors.Is(err, base) {
		t.Fatalf("the first stage named must stick: %s", StageOf(err))
	}

	if StageOf(base) != StageInfrastructure {
		t.Fatal("an unstaged error is not infrastructure")
	}
}
