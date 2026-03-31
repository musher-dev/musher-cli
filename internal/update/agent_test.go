package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveCheckResult(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	// Override the executable path so CurrentInstallContext doesn't fail
	original := executablePath

	t.Cleanup(func() { executablePath = original })

	executablePath = func() (string, error) {
		return "/usr/local/bin/musher", nil
	}

	err := SaveCheckResult("1.0.0", "2.0.0", "https://example.com/release")
	if err != nil {
		t.Fatalf("SaveCheckResult: %v", err)
	}

	// Verify state was saved
	state, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if state.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", state.LatestVersion, "2.0.0")
	}

	if state.CurrentVersion != "1.0.0" {
		t.Errorf("CurrentVersion = %q, want %q", state.CurrentVersion, "1.0.0")
	}

	if state.ReleaseURL != "https://example.com/release" {
		t.Errorf("ReleaseURL = %q, want %q", state.ReleaseURL, "https://example.com/release")
	}

	if state.InstallSource == "" {
		t.Error("InstallSource should not be empty")
	}
}

func TestTryStagedApplyExecPathUnavailable(t *testing.T) {
	t.Parallel()

	cfg := AgentConfig{
		AutoApply:      true,
		CurrentVersion: "1.0.0",
	}

	state := &State{
		StagedVersion: "2.0.0",
	}

	got := tryStagedApply(cfg, state, "", false, true)
	if got {
		t.Error("expected false when exec path unavailable")
	}

	if state.AutoApplyBlockedReason != "exec_path_unavailable" {
		t.Errorf("AutoApplyBlockedReason = %q, want %q", state.AutoApplyBlockedReason, "exec_path_unavailable")
	}

	if state.LastApplyError == "" {
		t.Error("LastApplyError should be set")
	}
}

func TestTryStagedApplyAllConditionsFalse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		autoApply       bool
		allowedBySource bool
		stagedVersion   string
		currentVersion  string
	}{
		{
			name:            "auto apply off",
			autoApply:       false,
			allowedBySource: true,
			stagedVersion:   "2.0.0",
			currentVersion:  "1.0.0",
		},
		{
			name:            "not allowed by source",
			autoApply:       true,
			allowedBySource: false,
			stagedVersion:   "2.0.0",
			currentVersion:  "1.0.0",
		},
		{
			name:            "no staged version",
			autoApply:       true,
			allowedBySource: true,
			stagedVersion:   "",
			currentVersion:  "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := AgentConfig{
				AutoApply:      tt.autoApply,
				CurrentVersion: tt.currentVersion,
			}

			state := &State{StagedVersion: tt.stagedVersion}
			got := tryStagedApply(cfg, state, "/usr/local/bin/musher", true, tt.allowedBySource)

			if got {
				t.Error("expected false")
			}
		})
	}
}

func TestStageUpdateSetsFields(t *testing.T) {
	t.Parallel()

	cfg := AgentConfig{AutoApply: true}
	state := &State{}
	info := &Info{LatestVersion: "3.0.0"}
	now := time.Now()

	stageUpdate(cfg, state, info, now, true)

	if state.StagedVersion != "3.0.0" {
		t.Errorf("StagedVersion = %q, want %q", state.StagedVersion, "3.0.0")
	}

	if state.StagedAt.IsZero() {
		t.Error("StagedAt should not be zero")
	}

	if state.AutoApplyBlockedReason != "" {
		t.Errorf("AutoApplyBlockedReason = %q, want empty", state.AutoApplyBlockedReason)
	}
}

func TestUpdateInstallSourceBlockedReason(t *testing.T) {
	t.Parallel()

	// Homebrew should set blocked reason
	state := &State{}
	allowed := updateInstallSource(state, "/opt/homebrew/Cellar/musher/1.0/bin/musher", true)

	if allowed {
		t.Error("homebrew should not be allowed")
	}

	if state.AutoApplyBlockedReason != blockedReasonManagedInstall {
		t.Errorf("AutoApplyBlockedReason = %q, want %q", state.AutoApplyBlockedReason, blockedReasonManagedInstall)
	}
}

func TestEnsureWritableWithElevationNeeded(t *testing.T) {
	t.Parallel()

	// When elevation is needed but ExecPathKnown is false, should return false
	ctx := InstallContext{
		ExecPathKnown:  false,
		NeedsElevation: true,
	}

	reExec, err := EnsureWritable(ctx)
	if err != nil {
		t.Fatalf("EnsureWritable: %v", err)
	}

	if reExec {
		t.Error("reExec should be false when ExecPathKnown is false")
	}
}

func TestNeedsElevationWritableDir(t *testing.T) {
	dir := t.TempDir()
	fakeBinary := filepath.Join(dir, "musher")

	if err := os.WriteFile(fakeBinary, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	if NeedsElevation(fakeBinary) {
		t.Error("NeedsElevation should be false for writable directory")
	}
}

func TestNeedsElevationNonExistentDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("NeedsElevation always returns false on Windows")
	}

	// Path in non-existent directory should need elevation
	result := NeedsElevation("/nonexistent/path/musher")
	if !result {
		t.Error("NeedsElevation should be true for non-existent directory")
	}
}

func TestSaveCheckResultWithRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	original := executablePath

	t.Cleanup(func() { executablePath = original })

	executablePath = func() (string, error) {
		return "/usr/local/bin/musher", nil
	}

	if err := SaveCheckResult("1.5.0", "2.0.0", "https://example.com/v2"); err != nil {
		t.Fatalf("SaveCheckResult: %v", err)
	}

	state, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if !state.LastCheckedAt.IsZero() && state.CurrentVersion != "1.5.0" {
		t.Errorf("CurrentVersion = %q, want %q", state.CurrentVersion, "1.5.0")
	}

	if state.LatestVersion != "2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", state.LatestVersion, "2.0.0")
	}
}

func TestRunAgentWithRealLock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_RUNTIME_DIR", dir)
	t.Setenv("MUSHER_STATE_HOME", filepath.Join(dir, "state"))

	// RunAgent with a valid version but updates disabled
	t.Setenv("MUSHER_UPDATE_DISABLED", "1")

	err := RunAgent(AgentConfig{
		CurrentVersion: "1.0.0",
		CheckInterval:  time.Hour,
	})
	if err != nil {
		t.Fatalf("RunAgent (disabled): %v", err)
	}
}
