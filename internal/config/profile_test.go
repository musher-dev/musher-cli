package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProfile_Default(t *testing.T) {
	t.Parallel()

	got := ResolveProfile("")
	if got != DefaultProfile {
		t.Errorf("ResolveProfile(\"\") = %q, want %q", got, DefaultProfile)
	}
}

func TestResolveProfile_Flag(t *testing.T) {
	t.Parallel()

	got := ResolveProfile("staging")
	if got != "staging" {
		t.Errorf("ResolveProfile(\"staging\") = %q, want %q", got, "staging")
	}
}

func TestResolveProfile_EnvVar(t *testing.T) {
	t.Setenv(ProfileEnvVar, "production")

	got := ResolveProfile("")
	if got != "production" {
		t.Errorf("expected 'production', got %q", got)
	}
}

func TestResolveProfile_FlagOverridesEnv(t *testing.T) {
	t.Setenv(ProfileEnvVar, "production")

	got := ResolveProfile("staging")
	if got != "staging" {
		t.Errorf("expected 'staging', got %q", got)
	}
}

func TestListProfiles_DefaultOnly(t *testing.T) {
	t.Setenv("MUSHER_CONFIG_HOME", t.TempDir())

	profiles, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}

	if len(profiles) != 1 || profiles[0] != DefaultProfile {
		t.Errorf("expected [default], got %v", profiles)
	}
}

func TestListProfiles_WithNamedProfiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("MUSHER_CONFIG_HOME", tmpDir)

	// Create two profile directories with config files.
	for _, name := range []string{"staging", "production"} {
		profileDir := filepath.Join(tmpDir, profilesDirName, name)
		if err := os.MkdirAll(profileDir, 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(profileDir, "config.yaml"), []byte("api:\n  url: test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	profiles, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}

	if len(profiles) != 3 {
		t.Fatalf("expected 3 profiles, got %d: %v", len(profiles), profiles)
	}

	if profiles[0] != DefaultProfile {
		t.Errorf("first profile should be default, got %q", profiles[0])
	}
}

func TestDeleteProfile_Default(t *testing.T) {
	err := DeleteProfile(DefaultProfile)
	if err == nil {
		t.Error("expected error when deleting default profile")
	}
}

func TestDeleteProfile_NonExistent(t *testing.T) {
	t.Setenv("MUSHER_CONFIG_HOME", t.TempDir())

	err := DeleteProfile("nonexistent")
	if err == nil {
		t.Error("expected error when deleting non-existent profile")
	}
}

func TestConfigActiveProfileName(t *testing.T) {
	t.Parallel()

	cfg := Load()

	if cfg.ActiveProfileName() != DefaultProfile {
		t.Errorf("expected default profile, got %q", cfg.ActiveProfileName())
	}
}
