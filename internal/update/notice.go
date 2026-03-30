package update

// NoticeKind describes the type of update notice to display.
type NoticeKind int

const (
	// NoticeNone indicates no notice should be shown.
	NoticeNone NoticeKind = iota
	// NoticeUpdateAvailable indicates a newer version can be installed.
	NoticeUpdateAvailable
	// NoticeStagedPending indicates a staged update will apply on a future run.
	NoticeStagedPending
	// NoticeHomebrewUpgrade indicates the user should use brew upgrade.
	NoticeHomebrewUpgrade
)

// Notice holds information for an update notice display.
type Notice struct {
	Kind           NoticeKind
	CurrentVersion string
	LatestVersion  string
	UpgradeHint    string
}

// CheckNotice loads persisted state and returns a Notice if the user should be
// informed about an available update. Returns nil when there is nothing to show.
func CheckNotice(currentVersion string) *Notice {
	if IsDisabled() || currentVersion == "" || currentVersion == "dev" {
		return nil
	}

	state, err := LoadState()
	if err != nil || !state.HasUpdate(currentVersion) {
		return nil
	}

	source := InstallSource(state.InstallSource)

	if source == InstallSourceHomebrew {
		return &Notice{
			Kind:           NoticeHomebrewUpgrade,
			CurrentVersion: currentVersion,
			LatestVersion:  state.LatestVersion,
			UpgradeHint:    UpgradeHint(source),
		}
	}

	if state.HasStagedUpdate(currentVersion) && state.AutoApplyBlockedReason == "" {
		return &Notice{
			Kind:           NoticeStagedPending,
			CurrentVersion: currentVersion,
			LatestVersion:  state.StagedVersion,
		}
	}

	return &Notice{
		Kind:           NoticeUpdateAvailable,
		CurrentVersion: currentVersion,
		LatestVersion:  state.LatestVersion,
	}
}
