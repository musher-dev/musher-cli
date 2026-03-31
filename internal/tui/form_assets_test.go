package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func testAssets() []bundledef.DiscoveredAsset {
	return []bundledef.DiscoveredAsset{
		{Asset: bundledef.Asset{ID: "code-review", Src: "skills/code-review/SKILL.md", Kind: "skill"}},
		{Asset: bundledef.Asset{ID: "reviewer", Src: "agents/reviewer.md", Kind: "agent"}},
		{Asset: bundledef.Asset{ID: "config", Src: "configs/default.yaml", Kind: "config"}, IDConflict: true},
	}
}

func TestAssetList_Defaults(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", testAssets(), &sty, &keys)

	if al.Label() != "Assets" {
		t.Errorf("Label() = %q, want %q", al.Label(), "Assets")
	}

	// All pre-selected.
	if al.Value() != "3 selected" {
		t.Errorf("Value() = %q, want %q", al.Value(), "3 selected")
	}

	if al.ValidationState() != ValValid {
		t.Errorf("ValidationState() = %d, want ValValid", al.ValidationState())
	}
}

func TestAssetList_SelectedAssets(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", testAssets(), &sty, &keys)

	assets := al.SelectedAssets()
	if len(assets) != 3 {
		t.Errorf("SelectedAssets() count = %d, want 3", len(assets))
	}
}

func TestAssetList_Toggle(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", testAssets(), &sty, &keys)
	al.Focus()

	// Toggle first item off.
	al.Update(tea.KeyPressMsg{Code: ' '})

	assets := al.SelectedAssets()
	if len(assets) != 2 {
		t.Errorf("SelectedAssets() after toggle = %d, want 2", len(assets))
	}

	// Toggle back on.
	al.Update(tea.KeyPressMsg{Code: ' '})

	assets = al.SelectedAssets()
	if len(assets) != 3 {
		t.Errorf("SelectedAssets() after re-toggle = %d, want 3", len(assets))
	}
}

func TestAssetList_NavigateAndToggle(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", testAssets(), &sty, &keys)
	al.Focus()

	// Navigate down.
	al.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	// Toggle second item.
	al.Update(tea.KeyPressMsg{Code: ' '})

	assets := al.SelectedAssets()
	if len(assets) != 2 {
		t.Errorf("SelectedAssets() = %d, want 2", len(assets))
	}

	// Verify the second asset (reviewer) was deselected.
	for _, a := range assets {
		if a.ID == "reviewer" {
			t.Error("expected 'reviewer' to be deselected")
		}
	}
}

func TestAssetList_Validation(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", testAssets(), &sty, &keys)
	al.Focus()

	// Deselect all.
	al.Update(tea.KeyPressMsg{Code: ' '})         // deselect 0
	al.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // move to 1
	al.Update(tea.KeyPressMsg{Code: ' '})         // deselect 1
	al.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // move to 2
	al.Update(tea.KeyPressMsg{Code: ' '})         // deselect 2

	errs := al.Validate()
	if len(errs) == 0 {
		t.Error("expected validation error when no assets selected")
	}

	if al.ValidationState() != ValInvalid {
		t.Errorf("ValidationState() = %d, want ValInvalid", al.ValidationState())
	}
}

func TestAssetList_Empty(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", nil, &sty, &keys)

	if !al.IsEmpty() {
		t.Error("expected IsEmpty() to be true")
	}

	if al.ValidationState() != ValNone {
		t.Errorf("ValidationState() = %d, want ValNone for empty list", al.ValidationState())
	}

	view := al.View(60)
	if !strings.Contains(view, "No assets found") {
		t.Error("expected empty state message in view")
	}
}

func TestAssetList_SetItems(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", nil, &sty, &keys)

	al.SetItems(testAssets())

	if al.IsEmpty() {
		t.Error("expected non-empty after SetItems")
	}

	if al.Value() != "3 selected" {
		t.Errorf("Value() = %q, want %q", al.Value(), "3 selected")
	}
}

func TestAssetList_ViewShowsIDConflict(t *testing.T) {
	t.Parallel()

	sty := newStyles(true)
	keys := defaultKeyMap()
	al := NewAssetList("Assets", testAssets(), &sty, &keys)

	view := al.View(80)
	if !strings.Contains(view, "ID conflict") {
		t.Error("expected ID conflict warning in view")
	}
}
