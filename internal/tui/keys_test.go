package tui

import (
	"testing"

	"charm.land/bubbles/v2/key"
)

func TestDefaultKeyMap(t *testing.T) {
	t.Parallel()

	keys := defaultKeyMap()

	tests := []struct {
		name    string
		binding key.Binding
		wantKey string
	}{
		{"up includes k", keys.Up, "k"},
		{"down includes j", keys.Down, "j"},
		{"enter", keys.Enter, "enter"},
		{"back is esc", keys.Back, "esc"},
		{"quit includes q", keys.Quit, "q"},
		{"search is slash", keys.Search, "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			boundKeys := tt.binding.Keys()
			found := false

			for _, k := range boundKeys {
				if k == tt.wantKey {
					found = true

					break
				}
			}

			if !found {
				t.Errorf("binding %q does not contain key %q, has %v", tt.name, tt.wantKey, boundKeys)
			}
		})
	}
}

func TestKeyMapHelpText(t *testing.T) {
	t.Parallel()

	keys := defaultKeyMap()

	if keys.Up.Help().Desc != "up" {
		t.Errorf("Up help desc = %q, want %q", keys.Up.Help().Desc, "up")
	}

	if keys.Down.Help().Desc != "down" {
		t.Errorf("Down help desc = %q, want %q", keys.Down.Help().Desc, "down")
	}

	if keys.Enter.Help().Desc != "select" {
		t.Errorf("Enter help desc = %q, want %q", keys.Enter.Help().Desc, "select")
	}

	if keys.Quit.Help().Desc != "quit" {
		t.Errorf("Quit help desc = %q, want %q", keys.Quit.Help().Desc, "quit")
	}
}
