package deployspec

import (
	"strings"
	"testing"
)

func TestValidateImageRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ref     string
		wantErr bool
		// reason documents WHY, so a future edit that flips a case has to
		// justify itself against the server's behavior rather than a vibe.
		reason string
	}{
		// Digest pins — always accepted, checked before anything else.
		{
			name:   "digest pin",
			ref:    "ghcr.io/acme/api@sha256:" + strings.Repeat("a", 64),
			reason: "immutable reference, the strongest guarantee",
		},
		{
			name:   "digest pin on a floating tag still wins",
			ref:    "ghcr.io/acme/api:latest@sha256:" + strings.Repeat("f", 64),
			reason: "server checks the digest regex first and returns early",
		},
		{
			name:   "digest with uppercase hex",
			ref:    "ghcr.io/acme/api@sha256:" + strings.Repeat("A", 64),
			reason: "the digest regex is [0-9a-f] so uppercase does not match it, but the ref then falls through to tag parsing where the text after the last colon is the hex string — not a floating tag, so the server accepts it. Mirrored rather than corrected.",
		},

		// Rejected: no tag at all.
		{
			name:    "no tag",
			ref:     "ghcr.io/acme/api",
			wantErr: true,
			reason:  "implicit :latest",
		},
		{
			name:    "empty",
			ref:     "",
			wantErr: true,
			reason:  "nothing to validate",
		},
		{
			name:    "whitespace only",
			ref:     "   ",
			wantErr: true,
			reason:  "trimmed to empty",
		},

		// Rejected: every floating tag in the upstream set.
		{name: "latest", ref: "ghcr.io/acme/api:latest", wantErr: true, reason: "floating"},
		{name: "main", ref: "ghcr.io/acme/api:main", wantErr: true, reason: "floating"},
		{name: "main-stable", ref: "ghcr.io/acme/api:main-stable", wantErr: true, reason: "floating"},
		{name: "master", ref: "ghcr.io/acme/api:master", wantErr: true, reason: "floating"},
		{name: "stable", ref: "ghcr.io/acme/api:stable", wantErr: true, reason: "floating"},
		{name: "edge", ref: "ghcr.io/acme/api:edge", wantErr: true, reason: "floating"},
		{name: "nightly", ref: "ghcr.io/acme/api:nightly", wantErr: true, reason: "floating"},
		{name: "dev", ref: "ghcr.io/acme/api:dev", wantErr: true, reason: "floating"},
		{name: "rolling", ref: "ghcr.io/acme/api:rolling", wantErr: true, reason: "floating"},
		{
			name:    "floating tag is case-insensitive",
			ref:     "ghcr.io/acme/api:LATEST",
			wantErr: true,
			reason:  "server lowercases before the set lookup",
		},

		// Accepted: anything not in the denylist. These are the cases a
		// semver allowlist would have wrongly rejected.
		{name: "semver tag", ref: "ghcr.io/acme/api:v1.2.3", reason: "conventional pin"},
		{name: "date tag", ref: "ghcr.io/acme/api:2026-04-01", reason: "date-stamp pin"},
		{
			name:   "pr tag",
			ref:    "ghcr.io/acme/api:pr-441",
			reason: "not floating; an allowlist would have rejected this",
		},
		{
			name:   "commit sha tag",
			ref:    "ghcr.io/acme/api:abc1234",
			reason: "not floating; an allowlist would have rejected this",
		},
		{
			name:   "tag merely containing a floating word",
			ref:    "ghcr.io/acme/api:latest-stable-v2",
			reason: "set membership is exact, not substring",
		},
		{
			name:   "docker hub short form with tag",
			ref:    "nginx:1.27",
			reason: "no registry host required",
		},

		// The upstream quirk, mirrored deliberately.
		{
			name:   "registry port read as a tag",
			ref:    "registry.example.com:5000/acme/api",
			reason: "server splits on the LAST colon of the whole ref, so '5000' is the tag and is not floating — accepted despite having no real tag. Mirrored bug-for-bug so the client is never stricter than the server.",
		},
		{
			name:    "registry port with a floating tag",
			ref:     "registry.example.com:5000/acme/api:latest",
			wantErr: true,
			reason:  "last colon yields 'latest'",
		},
		{
			name:   "registry port with a real tag",
			ref:    "registry.example.com:5000/acme/api:v2",
			reason: "last colon yields 'v2'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateImageRef(tt.ref)

			if tt.wantErr && err == nil {
				t.Errorf("ValidateImageRef(%q) = nil, want error\nreason: %s", tt.ref, tt.reason)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("ValidateImageRef(%q) = %v, want nil\nreason: %s", tt.ref, err, tt.reason)
			}
		})
	}
}

func TestValidateImageRefMessageIsActionable(t *testing.T) {
	t.Parallel()

	err := ValidateImageRef("ghcr.io/acme/api:latest")
	if err == nil {
		t.Fatal("expected an error for a floating tag")
	}

	// The message has to tell the user what to type instead; "invalid image"
	// would leave them guessing.
	for _, want := range []string{"latest", "v1.2.3", "sha256"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
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
