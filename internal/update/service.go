package update

import (
	"fmt"
	"strings"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

var executablePath = selfupdate.ExecutablePath

// InstallContext captures install provenance and whether self-update needs elevation.
type InstallContext struct {
	ExecPath       string
	Source         InstallSource
	NeedsElevation bool
	ExecPathKnown  bool
}

// CurrentInstallContext resolves the current binary path and install source.
func CurrentInstallContext() InstallContext {
	execPath, err := executablePath()
	if err != nil || strings.TrimSpace(execPath) == "" {
		return InstallContext{Source: InstallSourceUnknown}
	}

	return InstallContext{
		ExecPath:       execPath,
		Source:         DetectInstallSource(execPath),
		NeedsElevation: NeedsElevation(execPath),
		ExecPathKnown:  true,
	}
}

// EnsureWritable re-execs under sudo when the current binary cannot be replaced directly.
// It returns true when control should stop because a privileged re-exec was launched.
func EnsureWritable(ctx InstallContext) (bool, error) {
	if !ctx.ExecPathKnown || !ctx.NeedsElevation {
		return false, nil
	}

	if err := ReExecWithSudo(); err != nil {
		return false, fmt.Errorf("re-exec updater with sudo: %w", err)
	}

	return true, nil
}

// SaveCheckResult persists the last observed update state with the current install source.
func SaveCheckResult(current, latest, releaseURL string) error {
	install := CurrentInstallContext()

	state := &State{
		LastCheckedAt:  time.Now(),
		LatestVersion:  latest,
		CurrentVersion: current,
		ReleaseURL:     releaseURL,
		InstallSource:  string(install.Source),
	}

	return SaveState(state)
}
