// Package update provides self-update functionality for the Musher CLI.
//
// It wraps the go-selfupdate library to check for and apply updates
// from GitHub Releases with checksum verification.
package update

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	selfupdate "github.com/creativeprojects/go-selfupdate"
)

const repoSlug = "musher-dev/musher-cli"

// IsDisabled returns true if update checks are disabled via MUSHER_UPDATE_DISABLED.
func IsDisabled() bool {
	v := os.Getenv("MUSHER_UPDATE_DISABLED")
	if v == "1" || strings.EqualFold(v, "true") {
		return true
	}

	return false
}

// Info holds the result of a version check.
type Info struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	ReleaseURL      string `json:"releaseURL,omitempty"`

	// Release is the underlying release metadata (nil if not available).
	Release *selfupdate.Release `json:"-"`
}

// Updater manages checking for and applying updates.
type Updater struct {
	updater *selfupdate.Updater
}

// NewUpdater creates a new Updater configured for GitHub Releases.
func NewUpdater() (*Updater, error) {
	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{
		APIToken: os.Getenv("GITHUB_TOKEN"),
	})
	if err != nil {
		return nil, fmt.Errorf("create github source: %w", err)
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source:    source,
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	})
	if err != nil {
		return nil, fmt.Errorf("create updater: %w", err)
	}

	return &Updater{updater: updater}, nil
}

// CheckLatest checks if a newer version is available.
func (u *Updater) CheckLatest(ctx context.Context, currentVersion string) (*Info, error) {
	latest, found, err := u.updater.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
	if err != nil {
		return nil, fmt.Errorf("detect latest release: %w", err)
	}

	info := &Info{
		CurrentVersion: currentVersion,
	}

	if !found {
		info.LatestVersion = currentVersion
		return info, nil
	}

	info.LatestVersion = latest.Version()
	info.ReleaseURL = latest.URL
	info.Release = latest

	current, currentOK := parseSemver(currentVersion)
	if !currentOK {
		// Can't parse current version (e.g. "dev"), treat as needing update
		info.UpdateAvailable = true
		return info, nil
	}

	latestSemver, latestOK := parseSemver(latest.Version())
	if !latestOK {
		return info, nil
	}

	if latestSemver.GreaterThan(current) {
		info.UpdateAvailable = true
	}

	return info, nil
}

func parseSemver(raw string) (*semver.Version, bool) {
	version, err := semver.NewVersion(raw)
	if err != nil {
		return nil, false
	}

	return version, true
}

// Apply downloads and installs the given release, replacing the current binary.
func (u *Updater) Apply(ctx context.Context, release *selfupdate.Release) error {
	execPath, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("find executable path: %w", err)
	}

	if err := u.updater.UpdateTo(ctx, release, execPath); err != nil {
		return fmt.Errorf("apply update: %w", err)
	}

	return nil
}

// ApplyVersion downloads and installs a specific version.
func (u *Updater) ApplyVersion(ctx context.Context, version string) (*selfupdate.Release, error) {
	release, found, err := u.updater.DetectVersion(ctx, selfupdate.ParseSlug(repoSlug), version)
	if err != nil {
		return nil, fmt.Errorf("detect version %s: %w", version, err)
	}

	if !found {
		return nil, fmt.Errorf("version %s not found", version)
	}

	if err := u.Apply(ctx, release); err != nil {
		return nil, err
	}

	return release, nil
}
