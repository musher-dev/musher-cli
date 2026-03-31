package update

import (
	"testing"
)

func TestDetectInstallSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		binaryPath string
		want       InstallSource
	}{
		{
			name:       "homebrew cellar path",
			binaryPath: "/usr/local/Cellar/musher/1.0.0/bin/musher",
			want:       InstallSourceHomebrew,
		},
		{
			name:       "homebrew cellar path with homebrew prefix",
			binaryPath: "/opt/homebrew/Cellar/musher/1.0.0/bin/musher",
			want:       InstallSourceHomebrew,
		},
		{
			name:       "standalone binary",
			binaryPath: "/usr/local/bin/musher",
			want:       InstallSourceStandalone,
		},
		{
			name:       "go bin path",
			binaryPath: "/home/user/go/bin/musher",
			want:       InstallSourceStandalone,
		},
		{
			name:       "empty path",
			binaryPath: "",
			want:       InstallSourceUnknown,
		},
		{
			name:       "whitespace path",
			binaryPath: "   ",
			want:       InstallSourceUnknown,
		},
		{
			name:       "case insensitive cellar",
			binaryPath: "/usr/local/CELLAR/MUSHER/1.0.0/bin/musher",
			want:       InstallSourceHomebrew,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := DetectInstallSource(tt.binaryPath)
			if got != tt.want {
				t.Errorf("DetectInstallSource(%q) = %q, want %q", tt.binaryPath, got, tt.want)
			}
		})
	}
}

func TestAutoApplyAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source InstallSource
		want   bool
	}{
		{InstallSourceStandalone, true},
		{InstallSourceUnknown, true},
		{InstallSourceHomebrew, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			t.Parallel()

			got := AutoApplyAllowed(tt.source)
			if got != tt.want {
				t.Errorf("AutoApplyAllowed(%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

func TestUpgradeHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source InstallSource
		want   string
	}{
		{InstallSourceHomebrew, "brew upgrade musher"},
		{InstallSourceStandalone, ""},
		{InstallSourceUnknown, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			t.Parallel()

			got := UpgradeHint(tt.source)
			if got != tt.want {
				t.Errorf("UpgradeHint(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}
