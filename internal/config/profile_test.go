package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveProfile(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "flag takes precedence", flag: "from-flag", env: "from-env", want: "from-flag"},
		{name: "env var used when no flag", flag: "", env: "staging", want: "staging"},
		{name: "flag only", flag: "staging", env: "", want: "staging"},
		{name: "default when nothing set", flag: "", env: "", want: DefaultProfile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(ProfileEnvVar, tt.env)

			if got := ResolveProfile(tt.flag); got != tt.want {
				t.Errorf("ResolveProfile(%q) = %q, want %q", tt.flag, got, tt.want)
			}
		})
	}
}

func TestActiveProfile(t *testing.T) {
	t.Setenv(ProfileEnvVar, "")

	if got := ActiveProfile(); got != DefaultProfile {
		t.Errorf("ActiveProfile() = %q, want %q", got, DefaultProfile)
	}
}

func TestLoadWithProfile_Default(t *testing.T) {
	isolateConfig(t)

	cfg := LoadWithProfile("")
	if cfg == nil {
		t.Fatal("LoadWithProfile returned nil")
	}

	if got := cfg.ActiveProfileName(); got != DefaultProfile {
		t.Errorf("ActiveProfileName() = %q, want %q", got, DefaultProfile)
	}
}

func TestLoadWithProfile_Named(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, filepath.Join(dir, profilesDirName, "staging"),
		"api:\n  url: https://staging.musher.dev\n")

	cfg := LoadWithProfile("staging")
	if cfg == nil {
		t.Fatal("LoadWithProfile returned nil")
	}

	if got := cfg.ActiveProfileName(); got != "staging" {
		t.Errorf("ActiveProfileName() = %q, want %q", got, "staging")
	}

	if got := cfg.APIURL(); got != "https://staging.musher.dev" {
		t.Errorf("APIURL() = %q, want %q", got, "https://staging.musher.dev")
	}
}

func TestListProfiles(t *testing.T) {
	dir := isolateConfig(t)

	// No profiles directory yet — should return just "default".
	profiles, err := ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}

	if len(profiles) != 1 || profiles[0] != DefaultProfile {
		t.Fatalf("profiles = %v, want [%q]", profiles, DefaultProfile)
	}

	for _, name := range []string{"staging", "production"} {
		writeConfigFile(t, filepath.Join(dir, profilesDirName, name), "api:\n  url: test\n")
	}

	// A directory without a config file is not a profile.
	if mkErr := os.MkdirAll(filepath.Join(dir, profilesDirName, "empty"), 0o700); mkErr != nil {
		t.Fatal(mkErr)
	}

	profiles, err = ListProfiles()
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}

	want := []string{DefaultProfile, "production", "staging"}
	if len(profiles) != len(want) {
		t.Fatalf("profiles = %v, want %v", profiles, want)
	}

	for i := range want {
		if profiles[i] != want[i] {
			t.Errorf("profiles[%d] = %q, want %q", i, profiles[i], want[i])
		}
	}
}

func TestDeleteProfile(t *testing.T) {
	dir := isolateConfig(t)

	tests := []struct {
		name    string
		profile string
	}{
		{name: "default is protected", profile: DefaultProfile},
		{name: "empty name", profile: ""},
		{name: "nonexistent", profile: "nonexistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DeleteProfile(tt.profile); err == nil {
				t.Errorf("DeleteProfile(%q) = nil, want error", tt.profile)
			}
		})
	}

	profileDir := filepath.Join(dir, profilesDirName, "temp")
	writeConfigFile(t, profileDir, "api:\n  url: temp\n")

	if err := DeleteProfile("temp"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}

	if _, err := os.Stat(profileDir); !os.IsNotExist(err) {
		t.Error("profile directory still exists after delete")
	}
}

func TestSetForProfile(t *testing.T) {
	dir := isolateConfig(t)

	writeConfigFile(t, filepath.Join(dir, profilesDirName, "test"), "")

	cfg := LoadWithProfile("test")

	if err := cfg.SetForProfile("api.url", "https://custom.api.dev"); err != nil {
		t.Fatalf("SetForProfile: %v", err)
	}

	if got := cfg.GetString("api.url"); got != "https://custom.api.dev" {
		t.Errorf("GetString after SetForProfile = %q, want %q", got, "https://custom.api.dev")
	}

	// The value must land in the profile's file, not the base config.
	data, err := os.ReadFile(filepath.Join(dir, profilesDirName, "test", "config.yaml"))
	if err != nil {
		t.Fatalf("read profile config: %v", err)
	}

	if !strings.Contains(string(data), "https://custom.api.dev") {
		t.Errorf("profile config does not contain the written value:\n%s", data)
	}

	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); !os.IsNotExist(err) {
		t.Error("SetForProfile wrote to the base config file")
	}
}

func TestSetForProfile_DefaultWritesBaseConfig(t *testing.T) {
	dir := isolateConfig(t)

	cfg := LoadWithProfile(DefaultProfile)

	if err := cfg.SetForProfile("api.url", "https://base.api.dev"); err != nil {
		t.Fatalf("SetForProfile: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("read base config: %v", err)
	}

	if !strings.Contains(string(data), "https://base.api.dev") {
		t.Errorf("base config does not contain the written value:\n%s", data)
	}
}

func TestProfileConfigDir(t *testing.T) {
	dir := isolateConfig(t)

	got, err := ProfileConfigDir("staging")
	if err != nil {
		t.Fatalf("ProfileConfigDir: %v", err)
	}

	want := filepath.Join(dir, profilesDirName, "staging")
	if got != want {
		t.Errorf("ProfileConfigDir = %q, want %q", got, want)
	}
}

func TestConfigActiveProfileName(t *testing.T) {
	isolateConfig(t)

	cfg := Load()

	if got := cfg.ActiveProfileName(); got != DefaultProfile {
		t.Errorf("ActiveProfileName() = %q, want %q", got, DefaultProfile)
	}
}
