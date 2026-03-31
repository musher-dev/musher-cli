package update

import (
	"os"
	"path/filepath"
	"testing"
)

func writeStateFile(t *testing.T, dir, content string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "update-check.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckNotice_DevBuild(t *testing.T) {
	if notice := CheckNotice("dev"); notice != nil {
		t.Errorf("expected nil for dev build, got %+v", notice)
	}
}

func TestCheckNotice_Empty(t *testing.T) {
	if notice := CheckNotice(""); notice != nil {
		t.Errorf("expected nil for empty version, got %+v", notice)
	}
}

func TestCheckNotice_Disabled(t *testing.T) {
	t.Setenv("MUSHER_UPDATE_DISABLED", "1")

	if notice := CheckNotice("1.0.0"); notice != nil {
		t.Errorf("expected nil when disabled, got %+v", notice)
	}
}

func TestCheckNotice_NoState(t *testing.T) {
	t.Setenv("MUSHER_STATE_HOME", t.TempDir())

	if notice := CheckNotice("1.0.0"); notice != nil {
		t.Errorf("expected nil with no state file, got %+v", notice)
	}
}

func TestCheckNotice_UpToDate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)
	writeStateFile(t, dir, `{"latestVersion":"1.0.0","currentVersion":"1.0.0"}`)

	if notice := CheckNotice("1.0.0"); notice != nil {
		t.Errorf("expected nil when up to date, got %+v", notice)
	}
}

func TestCheckNotice_UpdateAvailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)
	writeStateFile(t, dir, `{"latestVersion":"2.0.0","currentVersion":"1.0.0","installSource":"standalone"}`)

	notice := CheckNotice("1.0.0")
	if notice == nil {
		t.Fatal("expected notice, got nil")
	}

	if notice.Kind != NoticeUpdateAvailable {
		t.Errorf("expected NoticeUpdateAvailable, got %d", notice.Kind)
	}

	if notice.LatestVersion != "2.0.0" {
		t.Errorf("expected latest 2.0.0, got %s", notice.LatestVersion)
	}
}

func TestCheckNotice_Homebrew(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)
	writeStateFile(t, dir, `{"latestVersion":"2.0.0","currentVersion":"1.0.0","installSource":"homebrew"}`)

	notice := CheckNotice("1.0.0")
	if notice == nil {
		t.Fatal("expected notice, got nil")
	}

	if notice.Kind != NoticeHomebrewUpgrade {
		t.Errorf("expected NoticeHomebrewUpgrade, got %d", notice.Kind)
	}

	if notice.UpgradeHint != "brew upgrade musher" {
		t.Errorf("expected brew upgrade hint, got %q", notice.UpgradeHint)
	}
}

func TestCheckNotice_StagedPending(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)
	writeStateFile(t, dir, `{
		"latestVersion":"2.0.0",
		"currentVersion":"1.0.0",
		"stagedVersion":"2.0.0",
		"installSource":"standalone"
	}`)

	notice := CheckNotice("1.0.0")
	if notice == nil {
		t.Fatal("expected notice, got nil")
	}

	if notice.Kind != NoticeStagedPending {
		t.Errorf("expected NoticeStagedPending, got %d", notice.Kind)
	}
}

func TestCheckNotice_StagedBlocked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MUSHER_STATE_HOME", dir)
	writeStateFile(t, dir, `{
		"latestVersion":"2.0.0",
		"currentVersion":"1.0.0",
		"stagedVersion":"2.0.0",
		"installSource":"standalone",
		"autoApplyBlockedReason":"elevation_required"
	}`)

	notice := CheckNotice("1.0.0")
	if notice == nil {
		t.Fatal("expected notice, got nil")
	}

	// When auto-apply is blocked, fall through to NoticeUpdateAvailable
	if notice.Kind != NoticeUpdateAvailable {
		t.Errorf("expected NoticeUpdateAvailable when blocked, got %d", notice.Kind)
	}
}
