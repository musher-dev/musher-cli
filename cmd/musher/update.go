package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/update"
)

func newUpdateCmd() *cobra.Command {
	var (
		targetVersion string
		force         bool
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update musher to the latest version",
		Long: `Update musher to the latest version from GitHub Releases.

Downloads the new binary, verifies its checksum, and replaces the current
executable. If the binary is not writable, sudo is requested automatically.

Set MUSHER_UPDATE_DISABLED=1 to disable update checks.`,
		Example: `  musher update
  musher update --version 1.2.3
  musher update --force`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runUpdate(cmd, out, targetVersion, force)
		},
	}

	cmd.Flags().StringVar(&targetVersion, "version", "", "Install a specific version (e.g. 1.2.3)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force update even if already up to date")

	return cmd
}

func runUpdate(cmd *cobra.Command, out *output.Writer, targetVersion string, force bool) error {
	ctx := cmd.Context()

	if update.IsDisabled() {
		out.Warning("Updates are disabled (MUSHER_UPDATE_DISABLED is set)")
		return nil
	}

	currentVersion := buildinfo.Version

	if currentVersion == "dev" && targetVersion == "" {
		out.Warning("Development build — cannot determine current version")
		out.Info("Install a release build: https://github.com/musher-dev/musher-cli/releases")

		return nil
	}

	updater, err := update.NewUpdater()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to initialize updater", err)
	}

	install := update.CurrentInstallContext()
	if install.Source == update.InstallSourceHomebrew {
		return clierrors.New(clierrors.ExitGeneral, "Self-update is disabled for Homebrew installs").
			WithHint("Run 'brew upgrade musher' instead")
	}

	// Specific version mode
	if targetVersion != "" {
		targetVersion = strings.TrimPrefix(targetVersion, "v")
		return updateToVersion(ctx, out, updater, targetVersion)
	}

	// Check for latest
	var spin *output.Spinner
	if !out.JSON {
		spin = out.Spinner("Checking for updates")
		spin.Start()
	}

	info, err := updater.CheckLatest(ctx, currentVersion)
	if err != nil {
		if spin != nil {
			spin.Stop()
		}

		cliErr := clierrors.Wrap(clierrors.ExitNetwork, "Failed to check for updates", err)
		if strings.Contains(err.Error(), "403") {
			cliErr = cliErr.WithHint("Set GITHUB_TOKEN to avoid rate limits")
		}

		return cliErr
	}

	if out.JSON {
		return out.PrintJSON(info)
	}

	if !info.UpdateAvailable && !force {
		spin.StopWithSuccess(fmt.Sprintf("Already up to date (v%s)", currentVersion))
		saveCheckState(currentVersion, info.LatestVersion, info.ReleaseURL)

		return nil
	}

	if info.Release == nil {
		spin.Stop()

		return clierrors.New(clierrors.ExitGeneral, "No release found for this platform").
			WithHint("Your OS/arch may not have a published binary")
	}

	if info.UpdateAvailable {
		spin.StopWithSuccess(fmt.Sprintf("Update available: v%s -> v%s", currentVersion, info.LatestVersion))
	} else {
		spin.StopWithSuccess(fmt.Sprintf("Reinstalling v%s", info.LatestVersion))
	}

	reexeced, err := ensureUpdateWritable(install)
	if err != nil {
		return err
	}

	if reexeced {
		return nil
	}

	spin = out.Spinner(fmt.Sprintf("Downloading v%s", info.LatestVersion))
	spin.Start()

	if err := updater.Apply(ctx, info.Release); err != nil {
		spin.Stop()

		return clierrors.Wrap(clierrors.ExitGeneral, "Update failed", err).
			WithHint("Try again or download manually from GitHub Releases")
	}

	spin.StopWithSuccess(fmt.Sprintf("Updated to v%s", info.LatestVersion))

	if info.ReleaseURL != "" {
		out.Muted("Release notes: %s", info.ReleaseURL)
	}

	saveCheckState(currentVersion, info.LatestVersion, info.ReleaseURL)

	return nil
}

func updateToVersion(ctx context.Context, out *output.Writer, updater *update.Updater, version string) error {
	reexeced, err := ensureUpdateWritable(update.CurrentInstallContext())
	if err != nil {
		return err
	}

	if reexeced {
		return nil
	}

	var spin *output.Spinner
	if !out.JSON {
		spin = out.Spinner(fmt.Sprintf("Installing v%s", version))
		spin.Start()
	}

	release, err := updater.ApplyVersion(ctx, version)
	if err != nil {
		if spin != nil {
			spin.Stop()
		}

		cliErr := clierrors.Wrap(clierrors.ExitGeneral, fmt.Sprintf("Failed to install v%s", version), err)
		if strings.Contains(err.Error(), "not found") {
			cliErr = cliErr.WithHint("Check available versions at https://github.com/musher-dev/musher-cli/releases")
		}

		return cliErr
	}

	if spin != nil {
		spin.StopWithSuccess(fmt.Sprintf("Installed v%s", release.Version()))
	}

	return nil
}

func saveCheckState(current, latest, releaseURL string) {
	_ = update.SaveCheckResult(current, latest, releaseURL)
}

func ensureUpdateWritable(install update.InstallContext) (bool, error) {
	reexeced, err := update.EnsureWritable(install)
	if err != nil {
		return false, clierrors.Wrap(clierrors.ExitGeneral, "Failed to re-exec updater with sudo", err)
	}

	return reexeced, nil
}
