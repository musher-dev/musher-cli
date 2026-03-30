package update

import (
	"os"
	"testing"
	"time"
)

func TestIsDisabled(t *testing.T) {
	tests := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"unset", "", false},
		{"1", "1", true},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"True", "True", true},
		{"0", "0", false},
		{"false", "false", false},
		{"random", "random", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MUSHER_UPDATE_DISABLED", tt.envVal)

			got := IsDisabled()
			if got != tt.want {
				t.Errorf("IsDisabled() = %v, want %v (env=%q)", got, tt.want, tt.envVal)
			}
		})
	}
}

func TestParseSemver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		wantOK bool
	}{
		{"simple version", "1.2.3", true},
		{"v prefix", "v1.2.3", true},
		{"prerelease", "1.2.3-beta.1", true},
		{"dev", "dev", false},
		{"empty", "", false},
		{"garbage", "notaversion", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v, ok := parseSemver(tt.raw)
			if ok != tt.wantOK {
				t.Errorf("parseSemver(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}

			if ok && v == nil {
				t.Error("parseSemver returned ok=true but nil version")
			}
		})
	}
}

func TestEnsureWritable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ctx        InstallContext
		wantReExec bool
	}{
		{
			name:       "unknown exec path",
			ctx:        InstallContext{ExecPathKnown: false},
			wantReExec: false,
		},
		{
			name:       "no elevation needed",
			ctx:        InstallContext{ExecPathKnown: true, NeedsElevation: false},
			wantReExec: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reExec, err := EnsureWritable(tt.ctx)
			if err != nil {
				t.Fatalf("EnsureWritable: %v", err)
			}

			if reExec != tt.wantReExec {
				t.Errorf("reExec = %v, want %v", reExec, tt.wantReExec)
			}
		})
	}
}

func TestUpdateInstallSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		execPath          string
		execPathAvailable bool
		wantAllowed       bool
		wantSource        string
	}{
		{
			name:              "standalone",
			execPath:          "/usr/local/bin/musher",
			execPathAvailable: true,
			wantAllowed:       true,
			wantSource:        "standalone",
		},
		{
			name:              "homebrew",
			execPath:          "/opt/homebrew/Cellar/musher/1.0/bin/musher",
			execPathAvailable: true,
			wantAllowed:       false,
			wantSource:        "homebrew",
		},
		{
			name:              "unknown when path unavailable",
			execPath:          "",
			execPathAvailable: false,
			wantAllowed:       true,
			wantSource:        "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			state := &State{}
			allowed := updateInstallSource(state, tt.execPath, tt.execPathAvailable)

			if allowed != tt.wantAllowed {
				t.Errorf("allowed = %v, want %v", allowed, tt.wantAllowed)
			}

			if state.InstallSource != tt.wantSource {
				t.Errorf("InstallSource = %q, want %q", state.InstallSource, tt.wantSource)
			}
		})
	}
}

func TestStageUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		autoApply         bool
		allowedBySource   bool
		wantBlockedReason string
	}{
		{
			name:              "auto apply disabled",
			autoApply:         false,
			allowedBySource:   true,
			wantBlockedReason: "auto_apply_disabled",
		},
		{
			name:              "managed install",
			autoApply:         true,
			allowedBySource:   false,
			wantBlockedReason: "managed_install",
		},
		{
			name:              "auto apply enabled and allowed",
			autoApply:         true,
			allowedBySource:   true,
			wantBlockedReason: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := AgentConfig{AutoApply: tt.autoApply}
			state := &State{}
			info := &Info{LatestVersion: "2.0.0"}
			now := time.Now()

			stageUpdate(cfg, state, info, now, tt.allowedBySource)

			if state.StagedVersion != "2.0.0" {
				t.Errorf("StagedVersion = %q, want %q", state.StagedVersion, "2.0.0")
			}

			if state.AutoApplyBlockedReason != tt.wantBlockedReason {
				t.Errorf("AutoApplyBlockedReason = %q, want %q", state.AutoApplyBlockedReason, tt.wantBlockedReason)
			}
		})
	}
}

func TestRunAgentDisabled(t *testing.T) {
	tests := []struct {
		name        string
		cfg         AgentConfig
		envDisabled string
	}{
		{
			name:        "disabled via env",
			cfg:         AgentConfig{CurrentVersion: "1.0.0"},
			envDisabled: "1",
		},
		{
			name: "empty current version",
			cfg:  AgentConfig{CurrentVersion: ""},
		},
		{
			name: "dev version",
			cfg:  AgentConfig{CurrentVersion: "dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envDisabled != "" {
				t.Setenv("MUSHER_UPDATE_DISABLED", tt.envDisabled)
			}

			err := RunAgent(tt.cfg)
			if err != nil {
				t.Errorf("RunAgent should return nil for disabled/skipped: %v", err)
			}
		})
	}
}

func TestTryStagedApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		autoApply         bool
		allowedBySource   bool
		stagedVersion     string
		currentVersion    string
		execPathAvailable bool
		want              bool
	}{
		{
			name:            "auto apply disabled",
			autoApply:       false,
			allowedBySource: true,
			stagedVersion:   "2.0.0",
			currentVersion:  "1.0.0",
			want:            false,
		},
		{
			name:            "not allowed by source",
			autoApply:       true,
			allowedBySource: false,
			stagedVersion:   "2.0.0",
			currentVersion:  "1.0.0",
			want:            false,
		},
		{
			name:            "no staged update",
			autoApply:       true,
			allowedBySource: true,
			stagedVersion:   "",
			currentVersion:  "1.0.0",
			want:            false,
		},
		{
			name:              "staged same version",
			autoApply:         true,
			allowedBySource:   true,
			stagedVersion:     "1.0.0",
			currentVersion:    "1.0.0",
			execPathAvailable: true,
			want:              false,
		},
		{
			name:              "exec path unavailable",
			autoApply:         true,
			allowedBySource:   true,
			stagedVersion:     "2.0.0",
			currentVersion:    "1.0.0",
			execPathAvailable: false,
			want:              false,
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
			got := tryStagedApply(cfg, state, "/usr/local/bin/musher", tt.execPathAvailable, tt.allowedBySource)

			if got != tt.want {
				t.Errorf("tryStagedApply() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCurrentInstallContext(t *testing.T) {
	// Override executablePath to return a known path
	original := executablePath

	t.Cleanup(func() { executablePath = original })

	t.Run("known path", func(t *testing.T) {
		executablePath = func() (string, error) {
			return "/usr/local/bin/musher", nil
		}

		ctx := CurrentInstallContext()
		if !ctx.ExecPathKnown {
			t.Error("ExecPathKnown = false, want true")
		}

		if ctx.ExecPath != "/usr/local/bin/musher" {
			t.Errorf("ExecPath = %q, want %q", ctx.ExecPath, "/usr/local/bin/musher")
		}

		if ctx.Source != InstallSourceStandalone {
			t.Errorf("Source = %q, want %q", ctx.Source, InstallSourceStandalone)
		}
	})

	t.Run("error resolving path", func(t *testing.T) {
		executablePath = func() (string, error) {
			return "", os.ErrNotExist
		}

		ctx := CurrentInstallContext()
		if ctx.ExecPathKnown {
			t.Error("ExecPathKnown = true, want false")
		}

		if ctx.Source != InstallSourceUnknown {
			t.Errorf("Source = %q, want %q", ctx.Source, InstallSourceUnknown)
		}
	})
}

func TestResolveExecPath(t *testing.T) {
	t.Parallel()

	// resolveExecPath should return a path in normal circumstances
	path, available := resolveExecPath()

	// In test environment the executable path should be resolvable
	if available && path == "" {
		t.Error("available=true but path is empty")
	}
}

func TestUpdateInstallSourceClearsBlockedReason(t *testing.T) {
	t.Parallel()

	state := &State{AutoApplyBlockedReason: blockedReasonManagedInstall}

	// Switching to standalone should clear the managed_install reason
	allowed := updateInstallSource(state, "/usr/local/bin/musher", true)
	if !allowed {
		t.Error("expected standalone to be allowed")
	}

	if state.AutoApplyBlockedReason != "" {
		t.Errorf("AutoApplyBlockedReason = %q, want empty after switching to standalone", state.AutoApplyBlockedReason)
	}
}

func TestInstallSourceConstants(t *testing.T) {
	t.Parallel()

	// Verify string values are stable (used in state files)
	if InstallSourceUnknown != "unknown" {
		t.Errorf("InstallSourceUnknown = %q, want %q", InstallSourceUnknown, "unknown")
	}

	if InstallSourceStandalone != "standalone" {
		t.Errorf("InstallSourceStandalone = %q, want %q", InstallSourceStandalone, "standalone")
	}

	if InstallSourceHomebrew != "homebrew" {
		t.Errorf("InstallSourceHomebrew = %q, want %q", InstallSourceHomebrew, "homebrew")
	}
}
