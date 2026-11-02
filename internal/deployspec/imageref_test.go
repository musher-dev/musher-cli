package deployspec

import (
	"strings"
	"testing"
)

var testDigest = "@sha256:" + strings.Repeat("a", 64)

// The platform accepts every tag (component v2 COMP-SRC-002), so the client
// checks grammar only. A client stricter than the server rejects references
// the platform would happily run.
func TestValidateImageRefIsGrammarOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{name: "digest pin", ref: "ghcr.io/acme/api" + testDigest},
		{name: "tag and digest", ref: "ghcr.io/acme/api:v1" + testDigest},
		{name: "semver tag", ref: "ghcr.io/acme/api:v1.2.3"},
		{name: "floating tag is accepted", ref: "ghcr.io/acme/api:latest"},
		{name: "no tag is accepted", ref: "ghcr.io/acme/api"},
		{name: "docker hub short form", ref: "nginx:1.27"},
		{name: "registry with port", ref: "registry.example.com:5000/acme/api:v2"},
		{name: "registry with port and no tag", ref: "registry.example.com:5000/acme/api"},
		{name: "surrounding whitespace is trimmed", ref: "  nginx:1.27  "},
		{name: "empty", ref: "", wantErr: true},
		{name: "whitespace only", ref: "   ", wantErr: true},
		{name: "uppercase repository path", ref: "ghcr.io/Acme/api:v1", wantErr: true},
		{name: "uppercase digest hex", ref: "ghcr.io/acme/api@sha256:" + strings.Repeat("A", 64), wantErr: true},
		{name: "short digest", ref: "ghcr.io/acme/api@sha256:abc", wantErr: true},
		{name: "space inside", ref: "ghcr.io/acme/api :v1", wantErr: true},
		{name: "tag too long", ref: "nginx:" + strings.Repeat("a", 129), wantErr: true},
		{name: "reference too long", ref: strings.Repeat("a", 513), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateImageRef(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateImageRef(%q) = %v, wantErr %v", tt.ref, err, tt.wantErr)
			}
		})
	}
}

func TestFloatingTagWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref  string
		want string
	}{
		{ref: "ghcr.io/acme/api:latest", want: "floating tag :latest"},
		{ref: "ghcr.io/acme/api:MAIN", want: "floating tag :MAIN"},
		{ref: "ghcr.io/acme/api", want: "has no tag"},
		{ref: "registry.example.com:5000/acme/api", want: "has no tag"},
		{ref: "registry.example.com:5000/acme/api:nightly", want: "floating tag :nightly"},
		{ref: "ghcr.io/acme/api:v1.2.3"},
		{ref: "ghcr.io/acme/api:latest-stable-v2"},
		{ref: "ghcr.io/acme/api:latest" + testDigest},
		{ref: "ghcr.io/acme/api" + testDigest},
		{ref: ""},
	}

	for _, tt := range tests {
		got := FloatingTagWarning(tt.ref)

		switch {
		case tt.want == "" && got != "":
			t.Errorf("FloatingTagWarning(%q) = %q, want none", tt.ref, got)
		case tt.want != "" && !strings.Contains(got, tt.want):
			t.Errorf("FloatingTagWarning(%q) = %q, want it to mention %q", tt.ref, got, tt.want)
		case tt.want != "" && !strings.Contains(got, "sha256"):
			t.Errorf("FloatingTagWarning(%q) = %q, want it to recommend a digest", tt.ref, got)
		}
	}
}

func TestImageTagIgnoresRegistryPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"nginx:1.27":                         "1.27",
		"registry.example.com:5000/acme/api": "",
		"registry.example.com:5000/api:v2":   "v2",
		"ghcr.io/acme/api:v1" + testDigest:   "v1",
		"ghcr.io/acme/api" + testDigest:      "",
	}

	for ref, want := range tests {
		if got := ImageTag(ref); got != want {
			t.Errorf("ImageTag(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestIsDigestPinned(t *testing.T) {
	t.Parallel()

	if !IsDigestPinned(" ghcr.io/acme/api:v1" + testDigest + " ") {
		t.Error("a digest reference is pinned")
	}

	if IsDigestPinned("ghcr.io/acme/api:v1") {
		t.Error("a tag is not a digest pin")
	}
}

func TestIsFloatingTag(t *testing.T) {
	t.Parallel()

	if !IsFloatingTag("LaTeSt") {
		t.Error("IsFloatingTag should be case-insensitive")
	}

	if IsFloatingTag("v1.0.0") {
		t.Error("a semver tag is not floating")
	}
}

func TestValidateRepository(t *testing.T) {
	t.Parallel()

	for _, good := range []string{"ghcr.io/acme/web", "localhost:5000/app", "acme/web"} {
		if err := ValidateRepository(good); err != nil {
			t.Errorf("ValidateRepository(%q) = %v", good, err)
		}
	}

	for _, bad := range []string{"", "ghcr.io/acme/web:v1", "ghcr.io/acme/web" + testDigest, "ghcr.io/ACME/web"} {
		if err := ValidateRepository(bad); err == nil {
			t.Errorf("ValidateRepository(%q) = nil, want error", bad)
		}
	}
}
