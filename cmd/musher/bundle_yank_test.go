package main

import (
	"testing"
)

func TestParseVersionRefValid(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		wantNS   string
		wantSlug string
		wantVer  string
	}{
		{
			name:     "standard ref",
			ref:      "acme/my-bundle:1.0.0",
			wantNS:   "acme",
			wantSlug: "my-bundle",
			wantVer:  "1.0.0",
		},
		{
			name:     "prerelease version",
			ref:      "org/pkg:2.0.0-beta.1",
			wantNS:   "org",
			wantSlug: "pkg",
			wantVer:  "2.0.0-beta.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, slug, ver, err := parseVersionRef(tt.ref)
			if err != nil {
				t.Fatalf("parseVersionRef(%q) error = %v", tt.ref, err)
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

func TestParseVersionRefInvalid(t *testing.T) {
	tests := []struct {
		name string
		ref  string
	}{
		{"no colon", "acme/my-bundle"},
		{"empty version", "acme/my-bundle:"},
		{"no slash", "my-bundle:1.0.0"},
		{"empty namespace", "/slug:1.0.0"},
		{"empty slug", "acme/:1.0.0"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseVersionRef(tt.ref)
			if err == nil {
				t.Fatalf("parseVersionRef(%q) = nil error, want error", tt.ref)
			}
		})
	}
}

func TestNewBundleYankCmdFlags(t *testing.T) {
	cmd := newBundleYankCmd()

	if cmd.Flags().Lookup("reason") == nil {
		t.Error("missing --reason flag")
	}

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("missing --yes flag")
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("missing -y shorthand for --yes")
	}
}

func TestNewBundleYankCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "yank"})
	if err != nil {
		t.Fatalf("Find(bundle yank) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle yank" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle yank")
	}
}

func TestNewBundleUnyankCmdPath(t *testing.T) {
	root := newRootCmd()

	cmd, _, err := root.Find([]string{"bundle", "unyank"})
	if err != nil {
		t.Fatalf("Find(bundle unyank) error = %v", err)
	}

	if got := cmd.CommandPath(); got != "musher bundle unyank" {
		t.Errorf("CommandPath() = %q, want %q", got, "musher bundle unyank")
	}
}

func TestParseVersionRefUsedByYank(t *testing.T) {
	// Test that parseVersionRef is what runYank depends on for validation
	_, _, _, err := parseVersionRef("invalid-ref")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}

	_, _, _, err = parseVersionRef("acme/test:1.0.0")
	if err != nil {
		t.Fatalf("unexpected error for valid ref: %v", err)
	}
}

func TestNewBundleUnyankCmdUse(t *testing.T) {
	cmd := newBundleUnyankCmd()
	if cmd.Use != "unyank <namespace/slug:version>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "unyank <namespace/slug:version>")
	}
}

func TestNewBundleUnyankCmdFlags(t *testing.T) {
	cmd := newBundleUnyankCmd()

	if cmd.Flags().Lookup("yes") == nil {
		t.Error("missing --yes flag")
	}

	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("missing -y shorthand for --yes")
	}
}

func TestNewBundleYankCmdUse(t *testing.T) {
	cmd := newBundleYankCmd()
	if cmd.Use != "yank <namespace/slug:version>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "yank <namespace/slug:version>")
	}
}
