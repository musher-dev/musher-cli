package bundledef_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func TestParseRefOptionalVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantNS   string
		wantSlug string
		wantVer  string
		wantErr  bool
	}{
		{
			name:     "namespace and slug only",
			input:    "acme/my-bundle",
			wantNS:   "acme",
			wantSlug: "my-bundle",
		},
		{
			name:     "namespace slug and version",
			input:    "acme/my-bundle:1.0.0",
			wantNS:   "acme",
			wantSlug: "my-bundle",
			wantVer:  "1.0.0",
		},
		{
			name:     "version with pre-release",
			input:    "org/tool:2.0.0-beta.1",
			wantNS:   "org",
			wantSlug: "tool",
			wantVer:  "2.0.0-beta.1",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "no slash",
			input:   "just-a-name",
			wantErr: true,
		},
		{
			name:    "empty namespace",
			input:   "/slug",
			wantErr: true,
		},
		{
			name:    "empty slug",
			input:   "namespace/",
			wantErr: true,
		},
		{
			name:    "empty version after colon",
			input:   "acme/bundle:",
			wantErr: true,
		},
		{
			name:    "too many path segments",
			input:   "a/b/c",
			wantErr: true,
		},
		{
			name:     "version with colon in semver build metadata",
			input:    "ns/slug:1.0.0+build.123",
			wantNS:   "ns",
			wantSlug: "slug",
			wantVer:  "1.0.0+build.123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := bundledef.ParseRefOptionalVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRefOptionalVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			if got.Namespace != tt.wantNS {
				t.Errorf("Namespace = %q, want %q", got.Namespace, tt.wantNS)
			}

			if got.Slug != tt.wantSlug {
				t.Errorf("Slug = %q, want %q", got.Slug, tt.wantSlug)
			}

			if got.Version != tt.wantVer {
				t.Errorf("Version = %q, want %q", got.Version, tt.wantVer)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	t.Parallel()

	t.Run("valid ref with version", func(t *testing.T) {
		t.Parallel()

		ref, err := bundledef.ParseRef("acme/bundle:1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if ref.Namespace != "acme" || ref.Slug != "bundle" || ref.Version != "1.0.0" {
			t.Errorf("got %+v", ref)
		}
	})

	t.Run("rejects ref without version", func(t *testing.T) {
		t.Parallel()

		_, err := bundledef.ParseRef("acme/bundle")
		if err == nil {
			t.Fatal("expected error for ref without version")
		}
	})
}

func TestParseRefNoVersion(t *testing.T) {
	t.Parallel()

	t.Run("valid ref without version", func(t *testing.T) {
		t.Parallel()

		ref, err := bundledef.ParseRefNoVersion("acme/bundle")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if ref.Namespace != "acme" || ref.Slug != "bundle" {
			t.Errorf("got %+v", ref)
		}
	})

	t.Run("rejects ref with version", func(t *testing.T) {
		t.Parallel()

		_, err := bundledef.ParseRefNoVersion("acme/bundle:1.0.0")
		if err == nil {
			t.Fatal("expected error for ref with version")
		}
	})
}

func TestRefString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref  bundledef.Ref
		want string
	}{
		{bundledef.Ref{Namespace: "acme", Slug: "tool"}, "acme/tool"},
		{bundledef.Ref{Namespace: "acme", Slug: "tool", Version: "1.0.0"}, "acme/tool:1.0.0"},
	}

	for _, tt := range tests {
		if got := tt.ref.String(); got != tt.want {
			t.Errorf("Ref%+v.String() = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestRefHasVersion(t *testing.T) {
	t.Parallel()

	if (bundledef.Ref{Version: "1.0"}).HasVersion() != true {
		t.Error("expected HasVersion() = true")
	}

	if (bundledef.Ref{}).HasVersion() != false {
		t.Error("expected HasVersion() = false")
	}
}
