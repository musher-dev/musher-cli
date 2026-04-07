package tui

import "testing"

func TestGlobalCommandIDsAreUnique(t *testing.T) {
	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &CommandDeps{
		HomeDeps: &HomeDeps{},
		Styles:   &sty,
		Keys:     &keys,
	}

	cmds := buildGlobalCommands(deps)
	if len(cmds) == 0 {
		t.Fatalf("expected non-empty global command list")
	}

	seen := map[string]struct{}{}

	for _, c := range cmds {
		if c.ID == "" {
			t.Errorf("command %q has empty ID", c.Title)
		}

		if c.Title == "" {
			t.Errorf("command %q has empty title", c.ID)
		}

		if c.Run == nil {
			t.Errorf("command %q has nil Run", c.ID)
		}

		if _, dup := seen[c.ID]; dup {
			t.Errorf("duplicate command id: %s", c.ID)
		}

		seen[c.ID] = struct{}{}
	}
}

func TestBuildGlobalCommandsNilDeps(t *testing.T) {
	if got := buildGlobalCommands(nil); got != nil {
		t.Errorf("expected nil for nil deps, got %+v", got)
	}
}
