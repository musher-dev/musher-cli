package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
)

func TestReportAuthStatusJSONShape(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "https://api.example.com")

	out, stdout, _ := newTestWriter()
	out.JSON = true

	orgs := []client.Organization{
		{ID: "o1", Name: "Acme", Handle: "acme"},
		{ID: "o2", Name: "Beta"},
	}

	if err := reportAuthStatus(out, config.Load(), auth.SourceKeyring, orgs, ""); err != nil {
		t.Fatalf("reportAuthStatus() error = %v", err)
	}

	var got authStatusResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON %q: %v", stdout.String(), err)
	}

	if !got.Authenticated {
		t.Error("expected authenticated=true")
	}

	if got.Source != string(auth.SourceKeyring) {
		t.Errorf("source = %q, want %q", got.Source, auth.SourceKeyring)
	}

	if got.APIURL != "https://api.example.com" {
		t.Errorf("apiUrl = %q, want %q", got.APIURL, "https://api.example.com")
	}

	if got.Profile != config.DefaultProfile {
		t.Errorf("profile = %q, want %q", got.Profile, config.DefaultProfile)
	}

	if len(got.Organizations) != 2 {
		t.Fatalf("len(organizations) = %d, want 2", len(got.Organizations))
	}

	if got.Organizations[0].ID != "o1" || got.Organizations[0].Handle != "acme" {
		t.Errorf("organizations[0] = %+v, want {o1 Acme acme}", got.Organizations[0])
	}

	if got.Organization == nil || got.Organization.ID != "o1" {
		t.Errorf("organization = %+v, want the first organization", got.Organization)
	}

	if got.Warning != "" {
		t.Errorf("warning = %q, want empty", got.Warning)
	}
}

// TestReportAuthStatusJSONDropsRetiredFields guards the shape change: the
// publisher-identity fields must not reappear in the payload.
func TestReportAuthStatusJSONDropsRetiredFields(t *testing.T) {
	out, stdout, _ := newTestWriter()
	out.JSON = true

	if err := reportAuthStatus(out, config.Load(), auth.SourceEnv, nil, ""); err != nil {
		t.Fatalf("reportAuthStatus() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("decode JSON %q: %v", stdout.String(), err)
	}

	for _, retired := range []string{"credentialName", "user", "namespaces"} {
		if _, present := raw[retired]; present {
			t.Errorf("retired field %q is still emitted", retired)
		}
	}

	if _, present := raw["organizations"]; !present {
		t.Error("missing organizations field")
	}
}

func TestReportAuthStatusJSONWarning(t *testing.T) {
	out, stdout, _ := newTestWriter()
	out.JSON = true

	const warning = "The credential lacks the 'organizations:read' permission"

	if err := reportAuthStatus(out, config.Load(), auth.SourceEnv, nil, warning); err != nil {
		t.Fatalf("reportAuthStatus() error = %v", err)
	}

	var got authStatusResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON %q: %v", stdout.String(), err)
	}

	if got.Warning != warning {
		t.Errorf("warning = %q, want %q", got.Warning, warning)
	}

	if got.Organization != nil {
		t.Errorf("organization = %+v, want nil", got.Organization)
	}

	if len(got.Organizations) != 0 {
		t.Errorf("len(organizations) = %d, want 0", len(got.Organizations))
	}
}

func TestReportAuthStatusTextListsOrganizations(t *testing.T) {
	out, stdout, _ := newTestWriter()

	orgs := []client.Organization{
		{ID: "o1", Name: "Acme", Handle: "acme"},
		{ID: "o2", Name: "Beta"},
	}

	if err := reportAuthStatus(out, config.Load(), auth.SourceKeyring, orgs, ""); err != nil {
		t.Fatalf("reportAuthStatus() error = %v", err)
	}

	got := stdout.String()

	for _, want := range []string{"Acme (acme)", "Beta"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got %q", want, got)
		}
	}
}
