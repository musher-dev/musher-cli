package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/doctor"
	"github.com/musher-dev/musher-cli/internal/harness"
)

func TestNewDoctorCmdRegistered(t *testing.T) {
	root := newRootCmd()

	found := false

	for _, cmd := range root.Commands() {
		if cmd.Name() == "doctor" {
			found = true

			break
		}
	}

	if !found {
		t.Fatal("doctor command not registered")
	}
}

func TestDoctorCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"doctor"})
	if err != nil {
		t.Fatalf("Find(doctor) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher doctor" {
		t.Fatalf("CommandPath() = %q, want %q", got, "musher doctor")
	}
}

func TestDoctorCmdRejectsArgs(t *testing.T) {
	cmd := newDoctorCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"extra"})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for extra args")
	}
}

func TestRunHarnessChecks_MapsStatuses(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	reg.Register(&harness.Provider{
		Spec: &harness.Spec{
			Name:        "installed",
			DisplayName: "Installed",
			Binary:      "go", // guaranteed on PATH
			Status: harness.StatusSpec{
				VersionArgs: []string{"version"},
			},
		},
		Available: func() bool { return true },
	})
	reg.Register(&harness.Provider{
		Spec: &harness.Spec{
			Name:        "missing",
			DisplayName: "Missing",
			Binary:      "musher-nonexistent-xyz",
			Status: harness.StatusSpec{
				InstallHint: "Install Missing from example.com",
			},
		},
		Available: func() bool { return false },
	})

	results := runHarnessChecks(t.Context(), reg)

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// Check that installed harness has a pass result.
	var foundInstalledPass bool

	for _, r := range results {
		if r.Name == "Installed / binary" && r.Status == doctor.StatusPass {
			foundInstalledPass = true
		}
	}

	if !foundInstalledPass {
		t.Error("expected pass result for installed harness binary check")
	}

	// Check that missing harness has a fail result with install hint.
	var foundMissingFail bool

	for _, r := range results {
		if r.Name == "Missing / binary" && r.Status == doctor.StatusFail {
			foundMissingFail = true

			if r.Detail != "Install Missing from example.com" {
				t.Errorf("detail = %q, want install hint", r.Detail)
			}
		}
	}

	if !foundMissingFail {
		t.Error("expected fail result for missing harness binary check")
	}
}

func TestRunHarnessChecks_WarnForMissingAuth(t *testing.T) {
	t.Parallel()

	reg := harness.NewRegistry()
	reg.Register(&harness.Provider{
		Spec: &harness.Spec{
			Name:        "authtest",
			DisplayName: "AuthTest",
			Binary:      "go",
			Status: harness.StatusSpec{
				AuthCheck: harness.AuthCheck{
					Path:        "/nonexistent/credentials.json",
					Description: "test credentials",
				},
			},
		},
		Available: func() bool { return true },
	})

	results := runHarnessChecks(t.Context(), reg)

	var foundAuthWarn bool

	for _, r := range results {
		if r.Name == "AuthTest / auth" && r.Status == doctor.StatusWarn {
			foundAuthWarn = true
		}
	}

	if !foundAuthWarn {
		t.Error("expected warn result for missing auth file")
	}
}
