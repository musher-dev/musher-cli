package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

func TestRunHubPublishMismatchedRef(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Write a musher.yaml with namespace/slug that differ from the CLI ref.
	yaml := []byte("namespace: alice\nslug: foo\nversion: 0.1.0\nname: Foo\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	err := runHubPublish(nil, out, "bob/bar")
	if err == nil {
		t.Fatal("expected error for mismatched ref, got nil")
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected CLIError, got %T: %v", err, err)
	}

	if cliErr.Code != clierrors.ExitUsage {
		t.Errorf("exit code = %d, want %d", cliErr.Code, clierrors.ExitUsage)
	}

	if !strings.Contains(cliErr.Message, "does not match") {
		t.Errorf("message = %q, want it to contain %q", cliErr.Message, "does not match")
	}

	if !strings.Contains(cliErr.Message, "alice/foo") {
		t.Errorf("message = %q, want it to contain local ref %q", cliErr.Message, "alice/foo")
	}

	if !strings.Contains(cliErr.Message, "bob/bar") {
		t.Errorf("message = %q, want it to contain requested ref %q", cliErr.Message, "bob/bar")
	}
}

func TestRunHubPublishMatchingRefValidatesHubReadiness(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Matching namespace/slug but missing hub-required fields (description, readme, license).
	yaml := []byte("namespace: acme\nslug: widget\nversion: 0.1.0\nname: Widget\n")
	if err := os.WriteFile(filepath.Join(dir, "musher.yaml"), yaml, 0o644); err != nil {
		t.Fatal(err)
	}

	out := testWriter()

	err := runHubPublish(nil, out, "acme/widget")
	if err == nil {
		t.Fatal("expected hub-readiness error, got nil")
	}

	if !strings.Contains(err.Error(), "not ready for Hub publishing") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "not ready for Hub publishing")
	}
}

func TestRunHubPublishNoLocalDefSkipsValidation(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("MUSHER_API_URL", "http://127.0.0.1:1") // isolate from host credentials

	out := testWriter()

	// No musher.yaml in dir — should skip local validation and hit auth error.
	err := runHubPublish(nil, out, "acme/widget")
	if err == nil {
		t.Fatal("expected auth error, got nil")
	}

	// Should NOT be a hub-readiness or mismatch error.
	if strings.Contains(err.Error(), "does not match") {
		t.Error("unexpected mismatch error when no local def exists")
	}

	if strings.Contains(err.Error(), "not ready for Hub publishing") {
		t.Error("unexpected hub-readiness error when no local def exists")
	}
}
