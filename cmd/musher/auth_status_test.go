package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/client"
)

func TestBuildAuthStatusJSON_Full(t *testing.T) {
	identity := &client.PublisherIdentity{
		User: &client.PublisherIdentityUser{
			Email:    "alice@example.com",
			FullName: "Alice",
		},
		Organization: &client.PublisherIdentityOrg{
			Name: "Acme Corp",
		},
		Namespaces: []client.NamespaceHandle{
			{Handle: "acme", DisplayName: "Acme"},
			{Handle: "personal", DisplayName: "Personal"},
		},
	}

	result := buildAuthStatusJSON(identity, "my-key", auth.SourceKeyring)

	if !result.Authenticated {
		t.Error("expected Authenticated=true")
	}

	if result.CredentialName != "my-key" {
		t.Errorf("CredentialName = %q, want %q", result.CredentialName, "my-key")
	}

	if result.Source != string(auth.SourceKeyring) {
		t.Errorf("Source = %q, want %q", result.Source, auth.SourceKeyring)
	}

	if result.User == nil {
		t.Fatal("expected User to be set")
	}

	if result.User.Email != "alice@example.com" {
		t.Errorf("User.Email = %q", result.User.Email)
	}

	if result.Organization != "Acme Corp" {
		t.Errorf("Organization = %q", result.Organization)
	}

	if len(result.Namespaces) != 2 {
		t.Fatalf("len(Namespaces) = %d, want 2", len(result.Namespaces))
	}

	if result.Namespaces[0].Handle != "acme" {
		t.Errorf("Namespaces[0].Handle = %q", result.Namespaces[0].Handle)
	}
}

func TestBuildAuthStatusJSON_NoUserNoOrg(t *testing.T) {
	identity := &client.PublisherIdentity{
		Namespaces: []client.NamespaceHandle{},
	}

	result := buildAuthStatusJSON(identity, "token", auth.SourceEnv)

	if result.User != nil {
		t.Error("expected User to be nil")
	}

	if result.Organization != "" {
		t.Errorf("Organization = %q, want empty", result.Organization)
	}

	if len(result.Namespaces) != 0 {
		t.Errorf("len(Namespaces) = %d, want 0", len(result.Namespaces))
	}
}
