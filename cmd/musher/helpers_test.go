package main

import (
	"testing"
)

func TestConfigForPublicClient(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "https://test.example.com")

	got := configForPublicClient(t.Context())
	if got != "https://test.example.com" {
		t.Errorf("configForPublicClient() = %q, want %q", got, "https://test.example.com")
	}
}

func TestNewPublicAPIClient(t *testing.T) {
	c := newPublicAPIClient("https://api.example.com")
	if c == nil {
		t.Fatal("newPublicAPIClient returned nil")
	}
}

func TestNewAPIClientNoCredentials(t *testing.T) {
	// Point at an unreachable API and clear any credentials.
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	_, _, err := newAPIClientFromContext(t.Context())
	if err == nil {
		t.Fatal("expected error when no credentials are available")
	}
}

func TestNewAPIClientWithKey(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "test-key-123")

	source, c, err := newAPIClientFromContext(t.Context())
	if err != nil {
		t.Fatalf("newAPIClientFromContext() error = %v", err)
	}

	if c == nil {
		t.Fatal("client is nil")
	}

	if source == "" {
		t.Error("source is empty")
	}
}

func TestRequireAuthNoCredentials(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "")

	_, err := requireAuthFromContext(t.Context())
	if err == nil {
		t.Fatal("expected error when no credentials are available")
	}
}

func TestRequireAuthWithKey(t *testing.T) {
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")
	t.Setenv("MUSHER_API_KEY", "test-key-123")

	c, err := requireAuthFromContext(t.Context())
	if err != nil {
		t.Fatalf("requireAuthFromContext() error = %v", err)
	}

	if c == nil {
		t.Fatal("client is nil")
	}
}
