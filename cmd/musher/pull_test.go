package main

import "testing"

func TestParseBundleRefOptionalVersion(t *testing.T) {
	tests := []struct {
		name      string
		ref       string
		wantNS    string
		wantSlug  string
		wantVer   string
		wantError bool
	}{
		{
			name:     "namespace and slug only",
			ref:      "acme/my-bundle",
			wantNS:   "acme",
			wantSlug: "my-bundle",
			wantVer:  "",
		},
		{
			name:     "namespace slug and version",
			ref:      "acme/my-bundle:1.0.0",
			wantNS:   "acme",
			wantSlug: "my-bundle",
			wantVer:  "1.0.0",
		},
		{
			name:     "version with pre-release",
			ref:      "acme/my-bundle:2.0.0-beta.1",
			wantNS:   "acme",
			wantSlug: "my-bundle",
			wantVer:  "2.0.0-beta.1",
		},
		{
			name:      "empty version after colon",
			ref:       "acme/my-bundle:",
			wantError: true,
		},
		{
			name:      "no slash",
			ref:       "my-bundle",
			wantError: true,
		},
		{
			name:      "empty namespace",
			ref:       "/my-bundle",
			wantError: true,
		},
		{
			name:      "empty slug",
			ref:       "acme/",
			wantError: true,
		},
		{
			name:      "empty string",
			ref:       "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, slug, ver, err := parseBundleRefOptionalVersion(tt.ref)

			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error for ref %q, got nil", tt.ref)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error for ref %q: %v", tt.ref, err)
			}

			if ns != tt.wantNS {
				t.Errorf("namespace = %q, want %q", ns, tt.wantNS)
			}

			if slug != tt.wantSlug {
				t.Errorf("slug = %q, want %q", slug, tt.wantSlug)
			}

			if ver != tt.wantVer {
				t.Errorf("version = %q, want %q", ver, tt.wantVer)
			}
		})
	}
}

func TestPullCommandRegistered(t *testing.T) {
	cmd := newPullCmd()

	if cmd.Use != "pull <namespace/slug[:version]>" {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}

	outputDirFlag := cmd.Flags().Lookup("output-dir")
	if outputDirFlag == nil {
		t.Fatal("expected --output-dir flag to be registered")
	}

	if outputDirFlag.Shorthand != "o" {
		t.Errorf("output-dir shorthand = %q, want %q", outputDirFlag.Shorthand, "o")
	}

	forceFlag := cmd.Flags().Lookup("force")
	if forceFlag == nil {
		t.Fatal("expected --force flag to be registered")
	}
}
