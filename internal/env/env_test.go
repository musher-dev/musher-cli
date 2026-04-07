package env_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/env"
)

func TestGetReturnsValue(t *testing.T) {
	t.Setenv("MUSHER_TEST_KEY", "hello")

	if got := env.Get("MUSHER_TEST_KEY"); got != "hello" {
		t.Errorf("Get = %q, want %q", got, "hello")
	}
}

func TestGetUnsetReturnsEmpty(t *testing.T) {
	if got := env.Get("MUSHER_TEST_DEFINITELY_UNSET"); got != "" {
		t.Errorf("Get unset = %q, want empty", got)
	}
}

func TestLookup(t *testing.T) {
	t.Setenv("MUSHER_TEST_KEY", "value")

	v, ok := env.Lookup("MUSHER_TEST_KEY")
	if !ok || v != "value" {
		t.Errorf("Lookup = (%q, %v), want (%q, true)", v, ok, "value")
	}

	if _, ok := env.Lookup("MUSHER_TEST_DEFINITELY_UNSET"); ok {
		t.Error("Lookup of unset key returned ok=true")
	}
}

func TestGetDefault(t *testing.T) {
	t.Setenv("MUSHER_TEST_KEY", "set")

	if got := env.GetDefault("MUSHER_TEST_KEY", "fallback"); got != "set" {
		t.Errorf("GetDefault set = %q, want %q", got, "set")
	}

	if got := env.GetDefault("MUSHER_TEST_DEFINITELY_UNSET", "fallback"); got != "fallback" {
		t.Errorf("GetDefault unset = %q, want %q", got, "fallback")
	}
}
