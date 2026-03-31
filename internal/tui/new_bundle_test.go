package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"github.com/musher-dev/musher-cli/internal/client"
)

func newTestNewBundleScreen() *newBundleScreen {
	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := &HomeDeps{}

	return newNewBundleScreen(context.Background(), deps, "/tmp/test-bundle", &sty, &keys)
}

func TestNewBundleScreen_Init(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()

	cmd := screen.Init()
	if cmd == nil {
		t.Fatal("expected Init to return a command for asset discovery")
	}
}

func TestNewBundleScreen_DefaultValues(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()

	ns := screen.form.FieldByLabel("Namespace")
	if ns.Value() != placeholderNamespace {
		t.Errorf("Namespace = %q, want %q", ns.Value(), placeholderNamespace)
	}

	slug := screen.form.FieldByLabel("Slug")
	if slug.Value() != "test-bundle" {
		t.Errorf("Slug = %q, want %q", slug.Value(), "test-bundle")
	}

	name := screen.form.FieldByLabel("Name")
	if name.Value() != "Test Bundle" {
		t.Errorf("Name = %q, want %q", name.Value(), "Test Bundle")
	}

	version := screen.form.FieldByLabel("Version")
	if version.Value() != "0.1.0" {
		t.Errorf("Version = %q, want %q", version.Value(), "0.1.0")
	}
}

func TestNewBundleScreen_AuthResult(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.authLoading = true

	identity := &client.PublisherIdentity{
		Namespaces: []client.NamespaceHandle{
			{Handle: "acme-corp", DisplayName: "Acme Corp"},
		},
	}

	screen.Update(newBundleAuthMsg{identity: identity})

	ns := screen.form.FieldByLabel("Namespace")
	if ns.Value() != "acme-corp" {
		t.Errorf("Namespace after auth = %q, want %q", ns.Value(), "acme-corp")
	}

	if screen.authLoading {
		t.Error("expected authLoading to be false")
	}
}

func TestNewBundleScreen_AuthMultipleNamespaces(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()

	identity := &client.PublisherIdentity{
		Namespaces: []client.NamespaceHandle{
			{Handle: "personal"},
			{Handle: "org-team"},
		},
	}

	screen.Update(newBundleAuthMsg{identity: identity})

	ns := screen.form.FieldByLabel("Namespace")
	if ns.Value() != "personal" {
		t.Errorf("Namespace = %q, want first namespace %q", ns.Value(), "personal")
	}

	if len(screen.namespaces) != 2 {
		t.Errorf("namespaces count = %d, want 2", len(screen.namespaces))
	}
}

func TestNewBundleScreen_AssetDiscovery(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.assetsLoading = true

	assets := []bundledef.DiscoveredAsset{
		{Asset: bundledef.Asset{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"}},
	}

	screen.Update(newBundleAssetsMsg{assets: assets})

	if screen.assetsLoading {
		t.Error("expected assetsLoading to be false")
	}

	selected := screen.assets.SelectedAssets()
	if len(selected) != 1 {
		t.Errorf("selected assets = %d, want 1", len(selected))
	}
}

func TestNewBundleScreen_BuildDef(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()

	// Set description.
	desc := screen.form.FieldByLabel("Description")
	desc.SetValue("A test bundle")

	// Add assets.
	screen.assets.SetItems([]bundledef.DiscoveredAsset{
		{Asset: bundledef.Asset{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"}},
	})

	def := screen.buildDef()

	if def.Namespace != placeholderNamespace {
		t.Errorf("Namespace = %q, want %q", def.Namespace, placeholderNamespace)
	}

	if def.Slug != "test-bundle" {
		t.Errorf("Slug = %q, want %q", def.Slug, "test-bundle")
	}

	if def.Name != "Test Bundle" {
		t.Errorf("Name = %q, want %q", def.Name, "Test Bundle")
	}

	if def.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", def.Version, "0.1.0")
	}

	if def.Visibility != "private" {
		t.Errorf("Visibility = %q, want %q", def.Visibility, "private")
	}

	if def.Description != "A test bundle" {
		t.Errorf("Description = %q, want %q", def.Description, "A test bundle")
	}

	if len(def.Assets) != 1 {
		t.Errorf("Assets count = %d, want 1", len(def.Assets))
	}
}

func TestNewBundleScreen_SlugNameAutoLink(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()

	// Change slug — name should auto-update.
	screen.slugField.SetValue("cool-project")
	screen.checkSlugNameLink()

	if screen.nameField.Value() != "Cool Project" {
		t.Errorf("Name after slug change = %q, want %q", screen.nameField.Value(), "Cool Project")
	}
}

func TestNewBundleScreen_NameManualEditBreaksLink(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.nameManuallyEdited = true

	screen.slugField.SetValue("cool-project")
	screen.checkSlugNameLink()

	// Name should NOT have changed.
	if screen.nameField.Value() != "Test Bundle" {
		t.Errorf("Name = %q, should still be %q after manual edit flag set", screen.nameField.Value(), "Test Bundle")
	}
}

func TestNewBundleScreen_View(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.width = 120
	screen.height = 40

	view := screen.View()
	if view == "" {
		t.Fatal("expected non-empty view")
	}

	if !strings.Contains(view, "New Bundle") {
		t.Error("expected 'New Bundle' in view")
	}
}

func TestNewBundleScreen_ViewResponsive(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.height = 30

	// Two-panel.
	screen.width = 120
	view := screen.View()

	if !strings.Contains(view, "Preview") {
		t.Error("expected Preview panel in two-panel mode")
	}

	// Single-panel.
	screen.width = 80
	view = screen.View()

	if view == "" {
		t.Error("expected non-empty view in single-panel mode")
	}

	// Compact.
	screen.width = 50
	view = screen.View()

	if view == "" {
		t.Error("expected non-empty view in compact mode")
	}

	// Minimal.
	screen.width = 30
	view = screen.View()

	if view == "" {
		t.Error("expected non-empty view in minimal mode")
	}
}

func TestNewBundleScreen_EscPops(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for esc")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestNewBundleScreen_SuccessState(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.submitted = true
	screen.successMsg = "Created musher.yaml"

	screen.width = 80
	screen.height = 30

	view := screen.View()
	if !strings.Contains(view, "Created") {
		t.Error("expected success message in view")
	}

	// Any key should trigger quit.
	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd after key in success state")
	}

	msg := cmd()
	if qMsg, ok := msg.(quitWithResultMsg); !ok {
		t.Errorf("expected quitWithResultMsg, got %T", msg)
	} else if qMsg.result.Action != "init" {
		t.Errorf("Action = %q, want %q", qMsg.result.Action, "init")
	}
}

func TestNewBundleScreen_PreviewToggle(t *testing.T) {
	t.Parallel()

	screen := newTestNewBundleScreen()
	screen.width = 80 // single-panel mode
	screen.height = 30

	if screen.showPreview {
		t.Error("expected showPreview false initially")
	}

	screen.Update(tea.KeyPressMsg{Code: 'p'})

	if !screen.showPreview {
		t.Error("expected showPreview true after p")
	}

	screen.Update(tea.KeyPressMsg{Code: 'p'})

	if screen.showPreview {
		t.Error("expected showPreview false after second p")
	}
}

func TestValidateSlugPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		valid bool
	}{
		{"my-bundle", true},
		{"a", true},
		{"my-cool-bundle-123", true},
		{"", true}, // empty handled by required check
		{"MY-BUNDLE", false},
		{"my bundle", false},
		{"my_bundle", false},
		{"-leading", false},
		{"trailing-", false},
	}

	for _, tt := range tests {
		errs := validateSlugPattern(tt.input)
		if tt.valid && len(errs) > 0 {
			t.Errorf("validateSlugPattern(%q) = %v, want valid", tt.input, errs)
		}

		if !tt.valid && len(errs) == 0 {
			t.Errorf("validateSlugPattern(%q) = valid, want errors", tt.input)
		}
	}
}

func TestValidateSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		valid bool
	}{
		{"0.1.0", true},
		{"1.0.0", true},
		{"1.2.3-beta.1", true},
		{"", true}, // empty handled by required
		{"1.0", false},
		{"v1", false},
	}

	for _, tt := range tests {
		errs := validateSemver(tt.input)
		if tt.valid && len(errs) > 0 {
			t.Errorf("validateSemver(%q) = %v, want valid", tt.input, errs)
		}

		if !tt.valid && len(errs) == 0 {
			t.Errorf("validateSemver(%q) = valid, want errors", tt.input)
		}
	}
}

func TestSlugToDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug     string
		expected string
	}{
		{"my-cool-bundle", "My Cool Bundle"},
		{"simple", "Simple"},
		{"a-b-c", "A B C"},
		{"", ""},
	}

	for _, tt := range tests {
		result := slugToDisplayName(tt.slug)
		if result != tt.expected {
			t.Errorf("slugToDisplayName(%q) = %q, want %q", tt.slug, result, tt.expected)
		}
	}
}
