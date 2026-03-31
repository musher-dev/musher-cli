package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
)

type mockAuthManager struct {
	source auth.CredentialSource
	apiKey string
	stored map[string]string // apiURL -> key
}

func (m *mockAuthManager) GetCredentials(_ string) (source auth.CredentialSource, apiKey string) {
	return m.source, m.apiKey
}

func (m *mockAuthManager) StoreAPIKey(apiURL, apiKey string) error {
	if m.stored == nil {
		m.stored = make(map[string]string)
	}

	m.stored[apiURL] = apiKey

	return nil
}

func (m *mockAuthManager) DeleteAPIKey(_ string) error {
	m.apiKey = ""
	m.source = auth.SourceNone

	return nil
}

func newTestAuthDeps(authMgr *mockAuthManager, checker AuthChecker) *HomeDeps {
	return &HomeDeps{
		Searcher: &mockSearcher{},
		Puller:   &mockPuller{},
		Auth:     checker,
		AuthMgr:  authMgr,
		APIURL:   "https://api.test.dev",
		Version:  "1.0.0",
	}
}

func newTestAuthScreen(authMgr *mockAuthManager, checker AuthChecker) *authScreen {
	sty := newStyles(true)
	keys := defaultKeyMap()
	deps := newTestAuthDeps(authMgr, checker)

	screen := newAuthScreen(context.Background(), deps, &sty, &keys)
	screen.width = 80
	screen.height = 24

	return screen
}

func TestAuthScreen_InitialState(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)

	if screen.state != authStateLoading {
		t.Errorf("initial state = %d, want %d (loading)", screen.state, authStateLoading)
	}

	cmd := screen.Init()
	if cmd == nil {
		t.Error("expected non-nil init cmd")
	}
}

func TestAuthScreen_LoadIdentity_Unauthenticated(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceNone, apiKey: ""}, nil)

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

	if screen.state != authStateUnauthenticated {
		t.Errorf("state = %d, want %d (unauthenticated)", screen.state, authStateUnauthenticated)
	}

	view := screen.View()
	if !strings.Contains(view, "Not authenticated") {
		t.Error("expected view to contain 'Not authenticated'")
	}

	if !strings.Contains(view, "Log in") {
		t.Error("expected view to contain 'Log in'")
	}
}

func TestAuthScreen_LoadIdentity_Authenticated(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceKeyring, apiKey: "key"}, nil)

	identity := &client.PublisherIdentity{
		CredentialName: "test-key",
		CredentialType: "publisher",
		User:           &client.PublisherIdentityUser{FullName: "Alice", Email: "alice@example.com"},
		Organization:   &client.PublisherIdentityOrg{Name: "Acme Corp"},
		Namespaces:     []client.NamespaceHandle{{Handle: "acme", DisplayName: "Acme Corp"}},
	}

	screen.Update(authIdentityLoadedMsg{identity: identity, source: auth.SourceKeyring})

	if screen.state != authStateAuthenticated {
		t.Errorf("state = %d, want %d (authenticated)", screen.state, authStateAuthenticated)
	}

	view := screen.View()
	if !strings.Contains(view, "Authenticated") {
		t.Error("expected view to contain 'Authenticated'")
	}

	if !strings.Contains(view, "Alice") {
		t.Error("expected view to contain user name")
	}

	if !strings.Contains(view, "alice@example.com") {
		t.Error("expected view to contain email")
	}

	if !strings.Contains(view, "Acme Corp") {
		t.Error("expected view to contain org name")
	}

	if !strings.Contains(view, "Log out") {
		t.Error("expected view to contain 'Log out'")
	}
}

func TestAuthScreen_EnvVarSource_NoLogout(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceEnv, apiKey: "key"}, nil)

	identity := &client.PublisherIdentity{
		CredentialName: "env-key",
		User:           &client.PublisherIdentityUser{FullName: "Bob"},
	}

	screen.Update(authIdentityLoadedMsg{identity: identity, source: auth.SourceEnv})

	view := screen.View()
	if !strings.Contains(view, "MUSHER_API_KEY") {
		t.Error("expected view to mention MUSHER_API_KEY env var")
	}

	// Enter should not trigger logout for env var source.
	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil cmd when pressing enter on env var auth")
	}

	if screen.state != authStateAuthenticated {
		t.Errorf("state should remain authenticated, got %d", screen.state)
	}
}

func TestAuthScreen_LoginFlow(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceNone, apiKey: ""}, nil)

	// Start unauthenticated.
	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

	// Press enter to open login input.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.state != authStateLoginInput {
		t.Fatalf("state = %d, want %d (loginInput)", screen.state, authStateLoginInput)
	}

	view := screen.View()
	if !strings.Contains(view, "Log in") {
		t.Error("expected breadcrumb to show 'Log in'")
	}

	if !strings.Contains(view, "Paste your API key") {
		t.Error("expected login instructions")
	}
}

func TestAuthScreen_LoginFlow_EscBack(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.state != authStateLoginInput {
		t.Fatalf("state = %d, want loginInput", screen.state)
	}

	// Press esc to go back.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if screen.state != authStateUnauthenticated {
		t.Errorf("state = %d, want unauthenticated after esc", screen.state)
	}
}

func TestAuthScreen_LoginValidateSuccess(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceNone, apiKey: ""}, nil)

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Simulate successful validation.
	identity := &client.PublisherIdentity{CredentialName: "new-key"}
	screen.Update(authValidateResultMsg{identity: identity})

	if screen.state != authStateAuthenticated {
		t.Errorf("state = %d, want authenticated", screen.state)
	}

	if screen.identity == nil || screen.identity.CredentialName != "new-key" {
		t.Error("expected identity to be set to new-key")
	}
}

func TestAuthScreen_LoginValidateError(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Simulate validation error.
	screen.Update(authValidateErrorMsg{err: errors.New("invalid key")})

	if screen.state != authStateLoginInput {
		t.Errorf("state = %d, want loginInput after error", screen.state)
	}

	if screen.validateErr == nil {
		t.Error("expected validateErr to be set")
	}

	view := screen.View()
	if !strings.Contains(view, "invalid key") {
		t.Error("expected view to show error message")
	}
}

func TestAuthScreen_LogoutFlow(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceKeyring, apiKey: "key"}, nil)

	identity := &client.PublisherIdentity{CredentialName: "my-key"}
	screen.Update(authIdentityLoadedMsg{identity: identity, source: auth.SourceKeyring})

	// Press enter to initiate logout.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.state != authStateConfirmLogout {
		t.Fatalf("state = %d, want confirmLogout", screen.state)
	}

	// Default is Cancel (cursor=1), move to Yes.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyUp})

	if screen.confirmCursor != 0 {
		t.Errorf("confirmCursor = %d, want 0 (Yes)", screen.confirmCursor)
	}

	view := screen.View()
	if !strings.Contains(view, "Remove stored credentials") {
		t.Error("expected confirm prompt")
	}

	// Confirm logout.
	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for logout")
	}
}

func TestAuthScreen_LogoutCancel(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceKeyring, apiKey: "key"}, nil)

	identity := &client.PublisherIdentity{CredentialName: "my-key"}
	screen.Update(authIdentityLoadedMsg{identity: identity, source: auth.SourceKeyring})

	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Default cursor is on Cancel. Press enter.
	screen.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if screen.state != authStateAuthenticated {
		t.Errorf("state = %d, want authenticated after cancel", screen.state)
	}
}

func TestAuthScreen_LogoutResult(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{source: auth.SourceKeyring, apiKey: "key"}, nil)

	identity := &client.PublisherIdentity{CredentialName: "my-key"}
	screen.Update(authIdentityLoadedMsg{identity: identity, source: auth.SourceKeyring})

	// Simulate successful logout.
	screen.Update(authLogoutResultMsg{err: nil})

	if screen.state != authStateUnauthenticated {
		t.Errorf("state = %d, want unauthenticated after logout", screen.state)
	}

	if screen.identity != nil {
		t.Error("expected identity to be cleared")
	}
}

func TestAuthScreen_QuitKey(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

	_, cmd := screen.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for quit")
	}

	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestAuthScreen_BackKey(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

	_, cmd := screen.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected non-nil cmd for back")
	}

	msg := cmd()
	if _, ok := msg.(popScreenMsg); !ok {
		t.Errorf("expected popScreenMsg, got %T", msg)
	}
}

func TestAuthScreen_TabTwoPanel(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)
	screen.width = 120

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

	if screen.focusArea != 0 {
		t.Fatalf("initial focusArea = %d, want 0", screen.focusArea)
	}

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 1 {
		t.Errorf("focusArea after tab = %d, want 1", screen.focusArea)
	}
}

func TestAuthScreen_TabSinglePanel(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)
	screen.width = 80

	screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

	screen.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if screen.focusArea != 0 {
		t.Errorf("focusArea should stay 0 in single-panel, got %d", screen.focusArea)
	}
}

func TestAuthScreen_WindowResize(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)

	screen.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if screen.width != 120 {
		t.Errorf("width = %d, want 120", screen.width)
	}

	if screen.height != 40 {
		t.Errorf("height = %d, want 40", screen.height)
	}
}

func TestAuthScreen_DetailContent_Authenticated(t *testing.T) {
	t.Parallel()

	screen := newTestAuthScreen(&mockAuthManager{}, nil)
	screen.width = 120

	identity := &client.PublisherIdentity{
		CredentialName: "my-key",
		CredentialType: "publisher",
		Namespaces: []client.NamespaceHandle{
			{Handle: "acme", DisplayName: "Acme Corp"},
			{Handle: "labs", DisplayName: "Labs"},
		},
	}

	screen.Update(authIdentityLoadedMsg{identity: identity, source: auth.SourceKeyring})

	view := screen.View()
	if !strings.Contains(view, "my-key") {
		t.Error("expected detail to show credential name")
	}

	if !strings.Contains(view, "acme") {
		t.Error("expected detail to show namespace")
	}

	if !strings.Contains(view, "api.test.dev") {
		t.Error("expected detail to show API URL")
	}
}

func TestAuthScreen_Layouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"two-panel", 120, 30},
		{"single", 80, 24},
		{"compact", 50, 20},
		{"minimal", 35, 15},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			screen := newTestAuthScreen(&mockAuthManager{}, nil)
			screen.width = tt.width
			screen.height = tt.height

			screen.Update(authIdentityLoadedMsg{identity: nil, source: auth.SourceNone})

			view := screen.View()
			if view == "" {
				t.Error("expected non-empty view")
			}

			if !strings.Contains(view, "Not authenticated") {
				t.Error("expected view to contain auth status")
			}
		})
	}
}
