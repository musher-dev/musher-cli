package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCommandIsEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  Command
		want bool
	}{
		{"nil Enabled defaults to enabled", Command{ID: "a"}, true},
		{"explicitly enabled", Command{ID: "b", Enabled: func() bool { return true }}, true},
		{"explicitly disabled", Command{ID: "c", Enabled: func() bool { return false }}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.cmd.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCommandGroupsAreDistinct(t *testing.T) {
	t.Parallel()

	groups := []string{CmdGroupResume, CmdGroupUse, CmdGroupCreate, CmdGroupManage, CmdGroupSystem}

	seen := make(map[string]struct{}, len(groups))

	for _, g := range groups {
		if g == "" {
			t.Error("command group title must not be empty")
		}

		if _, dup := seen[g]; dup {
			t.Errorf("duplicate command group title: %q", g)
		}

		seen[g] = struct{}{}
	}
}

func TestCommandGroupOrderIsCanonical(t *testing.T) {
	t.Parallel()

	ordered := []string{CmdGroupResume, CmdGroupUse, CmdGroupCreate, CmdGroupManage, CmdGroupSystem}

	for i := 1; i < len(ordered); i++ {
		if groupOrder(ordered[i-1]) >= groupOrder(ordered[i]) {
			t.Errorf("group %q should sort before %q", ordered[i-1], ordered[i])
		}
	}

	if groupOrder("something else") <= groupOrder(CmdGroupSystem) {
		t.Error("unknown groups should sort last")
	}
}

// commandProviderScreen asserts that one Commands() method satisfies both
// CommandProvider and CommandLister — they are two spellings of one contract.
type commandProviderScreen struct{ stubScreen }

func (c *commandProviderScreen) Commands() []Command {
	return []Command{{ID: "screen.local", Title: "Local", Run: func() tea.Cmd { return nil }}}
}

func TestCommandProviderAndListerAreInterchangeable(t *testing.T) {
	t.Parallel()

	var screen Screen = &commandProviderScreen{}

	provider, ok := screen.(CommandProvider)
	if !ok {
		t.Fatal("expected screen to satisfy CommandProvider")
	}

	if _, ok := screen.(CommandLister); !ok {
		t.Fatal("expected screen to satisfy CommandLister")
	}

	if len(provider.Commands()) != 1 {
		t.Errorf("expected 1 screen command, got %d", len(provider.Commands()))
	}
}
