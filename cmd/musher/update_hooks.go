package main

import (
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/update"
)

var agentWG sync.WaitGroup //nolint:gochecknoglobals // lifecycle coordination between pre/post run hooks

// maybeStartAgent kicks off the background update agent in a goroutine.
// It never blocks the calling goroutine.
func maybeStartAgent(currentVersion string) {
	if currentVersion == "" || currentVersion == versionDev || update.IsDisabled() {
		return
	}

	cfg := config.Load()
	agentCfg := update.AgentConfig{
		CurrentVersion: currentVersion,
		CheckInterval:  cfg.UpdateCheckInterval(),
		AutoApply:      cfg.UpdateAutoApply(),
	}

	agentWG.Add(1)

	go func() {
		defer agentWG.Done()

		_ = update.RunAgent(agentCfg) //nolint:errcheck // errors are logged and persisted in state
	}()
}

// waitForAgent waits up to timeout for the background agent goroutine to finish,
// so that freshly written state is available for the update notice.
func waitForAgent(timeout time.Duration) {
	done := make(chan struct{})

	go func() {
		agentWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// renderUpdateNotice displays an update notice to the user after command execution.
func renderUpdateNotice(out *output.Writer, currentVersion string) {
	if out.Quiet || out.JSON {
		return
	}

	waitForAgent(500 * time.Millisecond)

	notice := update.CheckNotice(currentVersion)
	if notice == nil {
		return
	}

	out.Println()

	switch notice.Kind {
	case update.NoticeHomebrewUpgrade:
		out.Info(
			"A new version of musher is available: v%s → v%s",
			notice.CurrentVersion, notice.LatestVersion,
		)
		out.Info("Run '%s' to update", notice.UpgradeHint)

	case update.NoticeStagedPending:
		out.Info(
			"A new version (v%s) has been staged and will apply on a future run",
			notice.LatestVersion,
		)

	case update.NoticeUpdateAvailable:
		out.Info(
			"A new version of musher is available: v%s → v%s",
			notice.CurrentVersion, notice.LatestVersion,
		)
		out.Info("Run 'musher update' to update")
	default:
		// NoticeNone — should not reach here since CheckNotice returns nil.
	}
}

// shouldShowUpdateNotice returns false for commands that should not display
// an update notice (because they handle their own messaging or produce
// machine-parseable output).
func shouldShowUpdateNotice(cmd *cobra.Command) bool {
	name := cmd.Name()

	switch name {
	case cmdNameUpdate, "completion", "__complete":
		return false
	}

	return true
}
