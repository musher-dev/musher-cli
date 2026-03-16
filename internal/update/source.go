package update

import (
	"path/filepath"
	"strings"
)

// InstallSource identifies how musher was installed.
type InstallSource string

const (
	// InstallSourceUnknown means install provenance could not be detected.
	InstallSourceUnknown InstallSource = "unknown"
	// InstallSourceStandalone means musher appears to be a standalone binary install.
	InstallSourceStandalone InstallSource = "standalone"
	// InstallSourceHomebrew means musher was installed via Homebrew formula.
	InstallSourceHomebrew InstallSource = "homebrew"
)

// DetectInstallSource infers the installation source from the executable path.
func DetectInstallSource(binaryPath string) InstallSource {
	paths := []string{binaryPath}
	if resolved, err := filepath.EvalSymlinks(binaryPath); err == nil {
		paths = append(paths, resolved)
	}

	for _, path := range paths {
		norm := strings.ToLower(filepath.ToSlash(path))
		if strings.Contains(norm, "/cellar/musher/") || strings.Contains(norm, "/homebrew/cellar/musher/") {
			return InstallSourceHomebrew
		}
	}

	if strings.TrimSpace(binaryPath) != "" {
		return InstallSourceStandalone
	}

	return InstallSourceUnknown
}

// AutoApplyAllowed reports whether background auto-apply is allowed for a source.
func AutoApplyAllowed(source InstallSource) bool {
	return source != InstallSourceHomebrew
}

// UpgradeHint returns a package-manager command when known.
func UpgradeHint(source InstallSource) string {
	switch source {
	case InstallSourceHomebrew:
		return "brew upgrade musher"
	default:
		return ""
	}
}
