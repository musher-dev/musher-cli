package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestShouldCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		lastCheckedAt time.Time
		interval      time.Duration
		want          bool
	}{
		{
			name:          "zero time always checks",
			lastCheckedAt: time.Time{},
			interval:      24 * time.Hour,
			want:          true,
		},
		{
			name:          "recent check skips",
			lastCheckedAt: time.Now().Add(-1 * time.Minute),
			interval:      24 * time.Hour,
			want:          false,
		},
		{
			name:          "old check triggers",
			lastCheckedAt: time.Now().Add(-25 * time.Hour),
			interval:      24 * time.Hour,
			want:          true,
		},
		{
			name:          "zero interval uses default (24h)",
			lastCheckedAt: time.Now().Add(-1 * time.Hour),
			interval:      0,
			want:          false,
		},
		{
			name:          "negative interval uses default (24h)",
			lastCheckedAt: time.Now().Add(-1 * time.Hour),
			interval:      -1,
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &State{LastCheckedAt: tt.lastCheckedAt}
			got := s.ShouldCheck(tt.interval)

			if got != tt.want {
				t.Errorf("ShouldCheck(%v) = %v, want %v", tt.interval, got, tt.want)
			}
		})
	}
}

func TestHasUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		latestVersion  string
		currentVersion string
		want           bool
	}{
		{"newer available", "1.2.0", "1.1.0", true},
		{"same version", "1.1.0", "1.1.0", false},
		{"older available", "1.0.0", "1.1.0", false},
		{"empty latest", "", "1.1.0", false},
		{"empty current", "1.2.0", "", false},
		{"both empty", "", "", false},
		{"invalid latest", "notaversion", "1.1.0", false},
		{"invalid current", "1.2.0", "notaversion", false},
		{"prerelease", "1.2.0-beta.1", "1.1.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &State{LatestVersion: tt.latestVersion}
			got := s.HasUpdate(tt.currentVersion)

			if got != tt.want {
				t.Errorf("HasUpdate(%q) = %v, want %v", tt.currentVersion, got, tt.want)
			}
		})
	}
}

func TestHasStagedUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		stagedVersion  string
		currentVersion string
		want           bool
	}{
		{"staged newer", "2.0.0", "1.0.0", true},
		{"staged same", "1.0.0", "1.0.0", false},
		{"staged older", "0.9.0", "1.0.0", false},
		{"staged empty", "", "1.0.0", false},
		{"invalid staged", "bad", "1.0.0", false},
		{"invalid current", "2.0.0", "bad", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &State{StagedVersion: tt.stagedVersion}
			got := s.HasStagedUpdate(tt.currentVersion)

			if got != tt.want {
				t.Errorf("HasStagedUpdate(%q) = %v, want %v", tt.currentVersion, got, tt.want)
			}
		})
	}
}

func TestClearStaged(t *testing.T) {
	t.Parallel()

	s := &State{
		StagedVersion: "2.0.0",
		StagedAt:      time.Now(),
	}

	s.ClearStaged()

	if s.StagedVersion != "" {
		t.Errorf("StagedVersion = %q, want empty", s.StagedVersion)
	}

	if !s.StagedAt.IsZero() {
		t.Errorf("StagedAt = %v, want zero", s.StagedAt)
	}
}

func TestDecodeState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		wantOK  bool
		checkFn func(t *testing.T, s *State)
	}{
		{
			name:   "valid json",
			data:   []byte(`{"latestVersion":"1.2.0","currentVersion":"1.1.0"}`),
			wantOK: true,
			checkFn: func(t *testing.T, s *State) {
				t.Helper()

				if s.LatestVersion != "1.2.0" {
					t.Errorf("LatestVersion = %q, want %q", s.LatestVersion, "1.2.0")
				}

				if s.CurrentVersion != "1.1.0" {
					t.Errorf("CurrentVersion = %q, want %q", s.CurrentVersion, "1.1.0")
				}
			},
		},
		{
			name:   "empty json object",
			data:   []byte(`{}`),
			wantOK: true,
		},
		{
			name:   "invalid json",
			data:   []byte(`{broken`),
			wantOK: false,
		},
		{
			name:   "empty bytes",
			data:   []byte{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state, ok := decodeState(tt.data)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}

			if ok && tt.checkFn != nil {
				tt.checkFn(t, state)
			}
		})
	}
}

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	original := &State{
		LastCheckedAt:  time.Now().Truncate(time.Second).UTC(),
		LatestVersion:  "2.0.0",
		CurrentVersion: "1.5.0",
		ReleaseURL:     "https://github.com/example/releases/v2.0.0",
		InstallSource:  "standalone",
	}

	if err := SaveState(original); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify the file was created (state root is MUSHER_STATE_HOME directly)
	stateFile := filepath.Join(dir, "state", stateFileName)
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	loaded, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.LatestVersion != original.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", loaded.LatestVersion, original.LatestVersion)
	}

	if loaded.CurrentVersion != original.CurrentVersion {
		t.Errorf("CurrentVersion = %q, want %q", loaded.CurrentVersion, original.CurrentVersion)
	}

	if loaded.ReleaseURL != original.ReleaseURL {
		t.Errorf("ReleaseURL = %q, want %q", loaded.ReleaseURL, original.ReleaseURL)
	}

	if loaded.InstallSource != original.InstallSource {
		t.Errorf("InstallSource = %q, want %q", loaded.InstallSource, original.InstallSource)
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	state, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if state.LatestVersion != "" {
		t.Errorf("expected empty state, got LatestVersion = %q", state.LatestVersion)
	}
}

func TestLoadStateCorruptedFile(t *testing.T) {
	dir := t.TempDir()
	stateRoot := filepath.Join(dir, "state")
	t.Setenv("MUSHER_STATE_HOME", stateRoot)

	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	// Write corrupted state
	if err := os.WriteFile(filepath.Join(stateRoot, stateFileName), []byte("{corrupted"), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState should not error on corrupt file: %v", err)
	}

	// Should return zero-value state
	if state.LatestVersion != "" {
		t.Errorf("expected empty state for corrupted file, got LatestVersion = %q", state.LatestVersion)
	}
}

func TestSaveStateCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "deep", "nested", "state"))

	state := &State{
		LastCheckedAt:  time.Now().Truncate(time.Second).UTC(),
		LatestVersion:  "1.0.0",
		CurrentVersion: "0.9.0",
	}

	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify file exists in the nested path.
	stateFile := filepath.Join(dir, "deep", "nested", "state", stateFileName)

	data, readErr := os.ReadFile(stateFile)
	if readErr != nil {
		t.Fatalf("state file not created at nested path: %v", readErr)
	}

	var loaded State
	if unmarshalErr := json.Unmarshal(data, &loaded); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}

	if loaded.LatestVersion != "1.0.0" {
		t.Errorf("LatestVersion = %q, want %q", loaded.LatestVersion, "1.0.0")
	}
}

func TestSaveStateOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	first := &State{
		LatestVersion:  "1.0.0",
		CurrentVersion: "0.9.0",
	}

	if err := SaveState(first); err != nil {
		t.Fatalf("SaveState (first): %v", err)
	}

	second := &State{
		LatestVersion:  "2.0.0",
		CurrentVersion: "1.0.0",
		ReleaseURL:     "https://example.com/v2",
	}

	if err := SaveState(second); err != nil {
		t.Fatalf("SaveState (second): %v", err)
	}

	loaded, loadErr := LoadState()
	if loadErr != nil {
		t.Fatalf("LoadState: %v", loadErr)
	}

	if loaded.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", loaded.LatestVersion, "2.0.0")
	}

	if loaded.ReleaseURL != "https://example.com/v2" {
		t.Errorf("ReleaseURL = %q, want %q", loaded.ReleaseURL, "https://example.com/v2")
	}
}

func TestSaveStateStagedFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	stagedAt := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	applyAt := time.Date(2025, 6, 1, 12, 5, 0, 0, time.UTC)

	state := &State{
		LatestVersion:          "3.0.0",
		StagedVersion:          "3.0.0",
		StagedAt:               stagedAt,
		LastApplyAttemptAt:     applyAt,
		LastApplyError:         "binary busy",
		AutoApplyBlockedReason: "managed_install",
	}

	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, loadErr := LoadState()
	if loadErr != nil {
		t.Fatalf("LoadState: %v", loadErr)
	}

	if loaded.StagedVersion != "3.0.0" {
		t.Errorf("StagedVersion = %q, want %q", loaded.StagedVersion, "3.0.0")
	}

	if loaded.LastApplyError != "binary busy" {
		t.Errorf("LastApplyError = %q, want %q", loaded.LastApplyError, "binary busy")
	}

	if loaded.AutoApplyBlockedReason != "managed_install" {
		t.Errorf("AutoApplyBlockedReason = %q, want %q", loaded.AutoApplyBlockedReason, "managed_install")
	}
}

func TestStatePathOrEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	path, ok := statePathOrEmpty()
	if !ok {
		t.Fatal("statePathOrEmpty returned ok=false with valid env")
	}

	if path == "" {
		t.Error("expected non-empty path")
	}

	if !filepath.IsAbs(path) {
		t.Errorf("path should be absolute, got %q", path)
	}
}

func TestStatePath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	path, err := statePath()
	if err != nil {
		t.Fatalf("statePath: %v", err)
	}

	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}

	if filepath.Base(path) != stateFileName {
		t.Errorf("expected filename %q, got %q", stateFileName, filepath.Base(path))
	}
}

func TestStateJSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := &State{
		LastCheckedAt:          time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		LatestVersion:          "3.0.0",
		CurrentVersion:         "2.5.0",
		ReleaseURL:             "https://example.com/release",
		StagedVersion:          "3.0.0",
		StagedAt:               time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		LastApplyAttemptAt:     time.Date(2025, 1, 15, 12, 5, 0, 0, time.UTC),
		LastApplyError:         "some error",
		InstallSource:          "homebrew",
		AutoApplyBlockedReason: "managed_install",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.LatestVersion != original.LatestVersion {
		t.Errorf("LatestVersion = %q, want %q", decoded.LatestVersion, original.LatestVersion)
	}

	if decoded.StagedVersion != original.StagedVersion {
		t.Errorf("StagedVersion = %q, want %q", decoded.StagedVersion, original.StagedVersion)
	}

	if decoded.AutoApplyBlockedReason != original.AutoApplyBlockedReason {
		t.Errorf("AutoApplyBlockedReason = %q, want %q", decoded.AutoApplyBlockedReason, original.AutoApplyBlockedReason)
	}
}
