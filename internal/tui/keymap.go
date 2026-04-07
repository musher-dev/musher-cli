package tui

import "charm.land/bubbles/v2/key"

// KeyGroup is a labeled cluster of bindings displayed together in the help
// overlay (e.g. "Navigation", "Actions", "Global"). Empty groups are skipped
// at render time.
type KeyGroup struct {
	Title    string
	Bindings []key.Binding
}

// KeyMap is the screen-scoped collection of key bindings exposed to the
// contextual footer (L0) and the help overlay (L1). It is constructed by each
// screen's KeyMap() method and is merged with globalKeyMap() before
// presentation.
type KeyMap struct {
	Groups []KeyGroup
}

// ShortHelp returns the flattened binding list in footer order. Disabled
// bindings (key.Binding.Enabled() == false) are filtered out so the footer
// never advertises an unavailable action.
func (k KeyMap) ShortHelp() []key.Binding {
	var out []key.Binding

	for _, group := range k.Groups {
		for _, binding := range group.Bindings {
			if !binding.Enabled() {
				continue
			}

			out = append(out, binding)
		}
	}

	return out
}

// FullHelp returns bindings grouped for the help overlay. Empty groups are
// skipped. Disabled bindings are retained but the overlay renderer is
// expected to dim them.
func (k KeyMap) FullHelp() [][]key.Binding {
	var out [][]key.Binding

	for _, group := range k.Groups {
		if len(group.Bindings) == 0 {
			continue
		}

		copied := make([]key.Binding, len(group.Bindings))
		copy(copied, group.Bindings)
		out = append(out, copied)
	}

	return out
}

// Merge combines two key maps. Groups are appended in order; bindings whose
// first key already exists in the receiver are dropped, so screen-scoped
// bindings always win over globals when there is a collision.
func (k KeyMap) Merge(other KeyMap) KeyMap {
	seen := make(map[string]struct{})

	for _, group := range k.Groups {
		for _, binding := range group.Bindings {
			if keys := binding.Keys(); len(keys) > 0 {
				seen[keys[0]] = struct{}{}
			}
		}
	}

	merged := KeyMap{Groups: make([]KeyGroup, 0, len(k.Groups)+len(other.Groups))}
	merged.Groups = append(merged.Groups, k.Groups...)

	for _, group := range other.Groups {
		filtered := make([]key.Binding, 0, len(group.Bindings))

		for _, binding := range group.Bindings {
			keys := binding.Keys()
			if len(keys) > 0 {
				if _, dup := seen[keys[0]]; dup {
					continue
				}

				seen[keys[0]] = struct{}{}
			}

			filtered = append(filtered, binding)
		}

		if len(filtered) > 0 {
			merged.Groups = append(merged.Groups, KeyGroup{Title: group.Title, Bindings: filtered})
		}
	}

	return merged
}

// Group titles used across the codebase. Keeping these as constants ensures
// the help overlay renders bindings under stable, recognizable headings.
const (
	GroupNavigation = "Navigation"
	GroupActions    = "Actions"
	GroupView       = "View"
	GroupGlobal     = "Global"
)

// Reusable bindings for the App-level dispatcher. These are intentionally
// separate from the legacy keyMap struct so they can be referenced before
// any screen exists.
var (
	// `/` is the primary palette binding. It is a single key with no
	// modifier, never intercepted by any IDE-integrated terminal (VSCode
	// captures Ctrl+P, Ctrl+K, and Ctrl+Shift+P as IDE chords before they
	// reach the child process), and visually evokes the "find" affordance
	// shared by browsers, vim, less, and most search-oriented TUIs. The
	// `:` key is kept as a vim-style alias, and Ctrl+K / Ctrl+P remain so
	// Linear/Raycast muscle memory carries over for users on standalone
	// terminals where those chords are still free. The App-level
	// dispatcher refuses to intercept these keys when the active screen
	// reports HasActiveInput()==true, so screens with focused text inputs
	// can still type the literal characters.
	dispatchPaletteBinding = key.NewBinding(
		key.WithKeys("/", ":", "ctrl+k", "ctrl+p"),
		key.WithHelp("/", "command palette"),
	)

	dispatchQuitBinding = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	)
)

// globalKeyMap returns the bindings that are always available regardless of
// the active screen. Used by tests and any future help-style surfaces.
func globalKeyMap() KeyMap {
	return KeyMap{
		Groups: []KeyGroup{
			{
				Title: GroupGlobal,
				Bindings: []key.Binding{
					dispatchPaletteBinding,
					dispatchQuitBinding,
				},
			},
		},
	}
}
