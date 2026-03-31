package provider_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/harness/provider"
)

func TestNewBuiltinRegistry(t *testing.T) {
	t.Parallel()

	reg := provider.NewBuiltinRegistry()

	want := []string{"claude", "codex", "copilot", "cursor", "gemini", "opencode"}
	got := reg.Names()

	if len(got) != len(want) {
		t.Fatalf("registry has %d providers, want %d", len(got), len(want))
	}

	for i, name := range want {
		if got[i] != name {
			t.Errorf("provider[%d] = %q, want %q", i, got[i], name)
		}
	}
}

func TestNewBuiltinRegistryAvailableDoesNotPanic(t *testing.T) {
	t.Parallel()

	reg := provider.NewBuiltinRegistry()

	for _, prov := range reg.List() {
		if prov.Available == nil {
			t.Errorf("provider %q has nil Available function", prov.Spec.Name)
			continue
		}

		// Call Available to ensure it doesn't panic (binary may or may not exist).
		_ = prov.Available()
	}
}
