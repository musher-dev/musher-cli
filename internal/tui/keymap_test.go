package tui

import (
	"testing"

	"charm.land/bubbles/v2/key"
)

func mkBinding(k, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(k), key.WithHelp(k, desc))
}

func TestKeyMapShortHelpFlattensInOrder(t *testing.T) {
	km := KeyMap{
		Groups: []KeyGroup{
			{Title: "A", Bindings: []key.Binding{mkBinding("x", "ex"), mkBinding("y", "wy")}},
			{Title: "B", Bindings: []key.Binding{mkBinding("z", "zee")}},
		},
	}

	got := km.ShortHelp()
	if len(got) != 3 {
		t.Fatalf("expected 3 bindings, got %d", len(got))
	}

	if got[0].Keys()[0] != "x" || got[1].Keys()[0] != "y" || got[2].Keys()[0] != "z" {
		t.Errorf("ShortHelp order wrong: %+v", got)
	}
}

func TestKeyMapShortHelpSkipsDisabled(t *testing.T) {
	disabled := mkBinding("d", "dis")
	disabled.SetEnabled(false)

	km := KeyMap{Groups: []KeyGroup{{Bindings: []key.Binding{mkBinding("a", "ay"), disabled}}}}

	got := km.ShortHelp()
	if len(got) != 1 || got[0].Keys()[0] != "a" {
		t.Errorf("expected only enabled bindings, got %+v", got)
	}
}

func TestKeyMapFullHelpSkipsEmptyGroups(t *testing.T) {
	km := KeyMap{
		Groups: []KeyGroup{
			{Title: "Empty"},
			{Title: "Full", Bindings: []key.Binding{mkBinding("a", "ay")}},
		},
	}

	got := km.FullHelp()
	if len(got) != 1 || got[0][0].Keys()[0] != "a" {
		t.Errorf("expected one group with binding 'a', got %+v", got)
	}
}

func TestKeyMapMergeDedupesByFirstKey(t *testing.T) {
	a := KeyMap{Groups: []KeyGroup{{Title: "A", Bindings: []key.Binding{mkBinding("x", "from-a")}}}}
	b := KeyMap{Groups: []KeyGroup{{Title: "B", Bindings: []key.Binding{
		mkBinding("x", "from-b"), // duplicate, should be dropped.
		mkBinding("y", "fresh"),
	}}}}

	merged := a.Merge(b)
	flat := merged.ShortHelp()

	if len(flat) != 2 {
		t.Fatalf("expected 2 bindings after dedup, got %d", len(flat))
	}

	if flat[0].Help().Desc != "from-a" {
		t.Errorf("receiver should win on collision, got %q", flat[0].Help().Desc)
	}

	if flat[1].Keys()[0] != "y" {
		t.Errorf("expected 'y' as second binding, got %q", flat[1].Keys()[0])
	}
}

func TestKeyMapMergeDropsEmptyMergedGroups(t *testing.T) {
	a := KeyMap{Groups: []KeyGroup{{Title: "A", Bindings: []key.Binding{mkBinding("x", "ex")}}}}
	b := KeyMap{Groups: []KeyGroup{{Title: "Dup", Bindings: []key.Binding{mkBinding("x", "dup")}}}}

	merged := a.Merge(b)
	if len(merged.Groups) != 1 {
		t.Errorf("expected the all-duplicate group to be dropped, got %d groups", len(merged.Groups))
	}
}

func TestGlobalKeyMapHasTwoBindings(t *testing.T) {
	km := globalKeyMap()
	if got := len(km.ShortHelp()); got != 2 {
		t.Errorf("expected 2 global bindings (/, ctrl+c), got %d", got)
	}
}
