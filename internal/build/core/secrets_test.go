package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeLookup(values map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		v, ok := values[key]

		return v, ok
	}
}

func TestResolveSecrets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "token.txt"), []byte("file-value"), 0o600); err != nil {
		t.Fatal(err)
	}

	abs := filepath.Join(t.TempDir(), "abs.txt")
	if err := os.WriteFile(abs, []byte("abs-value"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveSecrets(map[string]SecretSource{
		"FROM_ENV":  {Env: "MY_VAR"},
		"EMPTY_ENV": {Env: "EMPTY"},
		"FROM_FILE": {File: "token.txt"},
		"ABS_FILE":  {File: abs},
	}, root, fakeLookup(map[string]string{"MY_VAR": "env-value", "EMPTY": ""}))
	if err != nil {
		t.Fatalf("ResolveSecrets: %v", err)
	}

	want := map[string]string{"FROM_ENV": "env-value", "EMPTY_ENV": "", "FROM_FILE": "file-value", "ABS_FILE": "abs-value"}
	for id, value := range want {
		if string(got[id]) != value {
			t.Errorf("%s = %q, want %q", id, got[id], value)
		}
	}
}

func TestResolveSecretsErrorsNeverLeakValues(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	_, err := ResolveSecrets(map[string]SecretSource{"TOKEN": {Env: "UNSET_VAR"}}, root, fakeLookup(nil))
	if err == nil || !strings.Contains(err.Error(), "UNSET_VAR is not set") || !strings.Contains(err.Error(), "TOKEN") {
		t.Fatalf("missing env error = %v", err)
	}

	_, err = ResolveSecrets(map[string]SecretSource{"TOKEN": {File: "missing.txt"}}, root, fakeLookup(nil))
	if err == nil || !strings.Contains(err.Error(), "missing.txt") {
		t.Fatalf("missing file error = %v", err)
	}

	_, err = ResolveSecrets(map[string]SecretSource{"TOKEN": {}}, root, fakeLookup(nil))
	if err == nil || !strings.Contains(err.Error(), "no local source") {
		t.Fatalf("no source error = %v", err)
	}
}

func TestResolveSecretsDefaultLookupReadsProcessEnv(t *testing.T) {
	t.Setenv("MUSHER_TEST_BUILD_SECRET", "from-process")

	got, err := ResolveSecrets(map[string]SecretSource{"S": {Env: "MUSHER_TEST_BUILD_SECRET"}}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}

	if string(got["S"]) != "from-process" {
		t.Errorf("S = %q", got["S"])
	}
}

func TestRequestRef(t *testing.T) {
	t.Parallel()

	req := Request{Recipe: Recipe{Repository: "ghcr.io/acme/web"}, Tag: "abc123"}
	if got := req.Ref(); got != "ghcr.io/acme/web:abc123" {
		t.Errorf("Ref() = %q", got)
	}
}

func TestEmit(t *testing.T) {
	t.Parallel()

	// A nil channel drops events without blocking.
	Emit(t.Context(), nil, &Event{Kind: EventLog})

	events := make(chan Event, 1)
	Emit(t.Context(), events, &Event{Kind: EventLog, Message: "hi"})

	got := <-events
	if got.Message != "hi" || got.Time.IsZero() {
		t.Errorf("emitted %+v, want message and a stamped time", got)
	}

	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	Emit(t.Context(), events, &Event{Kind: EventLog, Time: stamp})

	if got := <-events; !got.Time.Equal(stamp) {
		t.Errorf("Time = %v, want %v", got.Time, stamp)
	}

	// A full channel and a canceled context must not block.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	full := make(chan Event)
	Emit(ctx, full, &Event{Kind: EventLog})
}
