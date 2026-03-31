package update

import (
	"context"
	"errors"
	"strings"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// AgentConfig controls background update behavior.
type AgentConfig struct {
	CurrentVersion string
	CheckInterval  time.Duration
	AutoApply      bool
}

const blockedReasonManagedInstall = "managed_install"

// errApplyBlocked indicates a staged apply did not succeed (state was saved).
var errApplyBlocked = errors.New("staged apply did not succeed")

// RunAgent performs a single background update tick.
func RunAgent(cfg AgentConfig) error {
	if IsDisabled() || cfg.CurrentVersion == "" || cfg.CurrentVersion == "dev" {
		return nil
	}

	return WithAgentLock(func() error {
		state, err := LoadState()
		if err != nil {
			return err
		}

		execPath, execPathAvailable := resolveExecPath()
		allowedBySource := updateInstallSource(state, execPath, execPathAvailable)

		if tryStagedApply(cfg, state, execPath, execPathAvailable, allowedBySource) {
			return nil
		}

		if !state.ShouldCheck(cfg.CheckInterval) {
			return SaveState(state)
		}

		return checkForUpdate(cfg, state, allowedBySource)
	})
}

func resolveExecPath() (string, bool) {
	execPath, execErr := selfupdate.ExecutablePath()
	available := execErr == nil && strings.TrimSpace(execPath) != ""

	return execPath, available
}

func updateInstallSource(state *State, execPath string, execPathAvailable bool) bool {
	source := InstallSourceUnknown
	if execPathAvailable {
		source = DetectInstallSource(execPath)
	}

	state.InstallSource = string(source)

	allowed := AutoApplyAllowed(source)
	if !allowed {
		state.AutoApplyBlockedReason = blockedReasonManagedInstall
	} else if state.AutoApplyBlockedReason == blockedReasonManagedInstall {
		state.AutoApplyBlockedReason = ""
	}

	return allowed
}

func tryStagedApply(cfg AgentConfig, state *State, execPath string, execPathAvailable, allowedBySource bool) bool {
	if !cfg.AutoApply || !allowedBySource || !state.HasStagedUpdate(cfg.CurrentVersion) {
		return false
	}

	if execPathAvailable {
		return applyStaged(state, execPath) == nil
	}

	state.LastApplyAttemptAt = time.Now()
	state.LastApplyError = "background apply skipped: executable path unavailable"
	state.AutoApplyBlockedReason = "exec_path_unavailable"

	return false
}

func checkForUpdate(cfg AgentConfig, state *State, allowedBySource bool) error {
	checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updater, err := NewUpdater()
	if err != nil {
		return SaveState(state)
	}

	info, err := updater.CheckLatest(checkCtx, cfg.CurrentVersion)
	if err != nil {
		return SaveState(state)
	}

	now := time.Now()
	state.LastCheckedAt = now
	state.LatestVersion = info.LatestVersion
	state.CurrentVersion = cfg.CurrentVersion
	state.ReleaseURL = info.ReleaseURL

	if info.UpdateAvailable {
		stageUpdate(cfg, state, info, now, allowedBySource)
	} else {
		state.ClearStaged()
		state.LastApplyError = ""
	}

	return SaveState(state)
}

func stageUpdate(cfg AgentConfig, state *State, info *Info, now time.Time, allowedBySource bool) {
	state.StagedVersion = info.LatestVersion
	state.StagedAt = now

	if !cfg.AutoApply {
		state.AutoApplyBlockedReason = "auto_apply_disabled"
	} else if !allowedBySource {
		state.AutoApplyBlockedReason = blockedReasonManagedInstall
	}
}

func applyStaged(state *State, execPath string) error {
	if execPath == "" {
		return errors.New("executable path unavailable")
	}

	if NeedsElevation(execPath) {
		state.LastApplyAttemptAt = time.Now()
		state.LastApplyError = "background apply requires elevated permissions"
		state.AutoApplyBlockedReason = "elevation_required"
		_ = SaveState(state) //nolint:errcheck // best-effort state persistence

		return errApplyBlocked
	}

	applyCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	updater, err := NewUpdater()
	if err != nil {
		state.LastApplyAttemptAt = time.Now()
		state.LastApplyError = err.Error()
		state.AutoApplyBlockedReason = "apply_error"
		_ = SaveState(state) //nolint:errcheck // best-effort state persistence

		return errApplyBlocked
	}

	_, err = updater.ApplyVersion(applyCtx, state.StagedVersion)
	state.LastApplyAttemptAt = time.Now()

	if err != nil {
		state.LastApplyError = err.Error()
		state.AutoApplyBlockedReason = "apply_error"
		_ = SaveState(state) //nolint:errcheck // best-effort state persistence

		return errApplyBlocked
	}

	state.LastApplyError = ""
	state.CurrentVersion = state.StagedVersion
	state.LatestVersion = state.StagedVersion
	state.AutoApplyBlockedReason = ""
	state.ClearStaged()

	return SaveState(state)
}
