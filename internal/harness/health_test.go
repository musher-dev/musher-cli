package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckHealth_BinaryNotFound(t *testing.T) {
	t.Parallel()

	prov := &Provider{
		Spec: &Spec{
			Name:        "fake",
			DisplayName: "Fake Harness",
			Binary:      "musher-nonexistent-binary-xyz",
		},
	}

	report := CheckHealth(t.Context(), prov)

	if report.Installed {
		t.Error("expected Installed=false for missing binary")
	}

	if report.ProviderName != "fake" {
		t.Errorf("ProviderName = %q, want %q", report.ProviderName, "fake")
	}

	if len(report.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(report.Checks))
	}

	if report.Checks[0].Status != CheckFail {
		t.Errorf("binary check status = %d, want CheckFail", report.Checks[0].Status)
	}
}

func TestCheckHealth_BinaryFound(t *testing.T) {
	t.Parallel()

	// Use "go" which is guaranteed to be on PATH in a Go test environment.
	prov := &Provider{
		Spec: &Spec{
			Name:        "gotest",
			DisplayName: "Go Test",
			Binary:      "go",
			Status: StatusSpec{
				VersionArgs: []string{"version"},
			},
		},
	}

	report := CheckHealth(t.Context(), prov)

	if !report.Installed {
		t.Error("expected Installed=true for 'go' binary")
	}

	if report.Version == "" {
		t.Error("expected non-empty Version")
	}

	// Should have binary + version checks.
	if len(report.Checks) < 2 {
		t.Fatalf("expected at least 2 checks, got %d", len(report.Checks))
	}

	if report.Checks[0].Name != "binary" || report.Checks[0].Status != CheckPass {
		t.Errorf("binary check: name=%q status=%d", report.Checks[0].Name, report.Checks[0].Status)
	}

	if report.Checks[1].Name != "version" || report.Checks[1].Status != CheckPass {
		t.Errorf("version check: name=%q status=%d", report.Checks[1].Name, report.Checks[1].Status)
	}
}

func TestCheckHealth_AuthCheckPath(t *testing.T) {
	t.Parallel()

	// Create a temp file to act as a credentials file.
	tmp := t.TempDir()
	credFile := filepath.Join(tmp, "creds.json")

	if err := os.WriteFile(credFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &Provider{
		Spec: &Spec{
			Name:        "gotest",
			DisplayName: "Go Test",
			Binary:      "go",
			Status: StatusSpec{
				AuthCheck: AuthCheck{
					Path:        credFile,
					Description: "test credentials",
				},
			},
		},
	}

	report := CheckHealth(t.Context(), prov)

	// Find auth check.
	var authCheck *HealthCheck

	for i := range report.Checks {
		if report.Checks[i].Name == "auth" {
			authCheck = &report.Checks[i]

			break
		}
	}

	if authCheck == nil {
		t.Fatal("auth check not found in report")
	}

	if authCheck.Status != CheckPass {
		t.Errorf("auth check status = %d, want CheckPass; message: %s", authCheck.Status, authCheck.Message)
	}
}

func TestCheckHealth_AuthCheckMissing(t *testing.T) {
	t.Parallel()

	prov := &Provider{
		Spec: &Spec{
			Name:        "gotest",
			DisplayName: "Go Test",
			Binary:      "go",
			Status: StatusSpec{
				AuthCheck: AuthCheck{
					Path:        "/nonexistent/path/creds.json",
					Description: "test credentials",
				},
			},
		},
	}

	report := CheckHealth(t.Context(), prov)

	var authCheck *HealthCheck

	for i := range report.Checks {
		if report.Checks[i].Name == "auth" {
			authCheck = &report.Checks[i]

			break
		}
	}

	if authCheck == nil {
		t.Fatal("auth check not found in report")
	}

	if authCheck.Status != CheckWarn {
		t.Errorf("auth check status = %d, want CheckWarn; message: %s", authCheck.Status, authCheck.Message)
	}
}

func TestCheckAllHealth_Concurrent(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.Register(&Provider{
		Spec: &Spec{
			Name:        "alpha",
			DisplayName: "Alpha",
			Binary:      "musher-nonexistent-alpha",
		},
		Available: func() bool { return false },
	})
	reg.Register(&Provider{
		Spec: &Spec{
			Name:        "beta",
			DisplayName: "Beta",
			Binary:      "go",
			Status: StatusSpec{
				VersionArgs: []string{"version"},
			},
		},
		Available: func() bool { return true },
	})

	results := CheckAllHealth(t.Context(), reg)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Results should be sorted by name.
	if results[0].ProviderName != "alpha" {
		t.Errorf("first result = %q, want %q", results[0].ProviderName, "alpha")
	}

	if results[1].ProviderName != "beta" {
		t.Errorf("second result = %q, want %q", results[1].ProviderName, "beta")
	}

	if results[0].Installed {
		t.Error("alpha should not be installed")
	}

	if !results[1].Installed {
		t.Error("beta should be installed")
	}
}
