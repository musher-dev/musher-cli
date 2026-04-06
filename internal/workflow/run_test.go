package workflow

import "testing"

func TestParseBundleRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		wantNS   string
		wantSlug string
		wantVer  string
		wantErr  bool
	}{
		{"namespace/slug", "myns/myslug", "myns", "myslug", "", false},
		{"with version", "myns/myslug:1.0.0", "myns", "myslug", "1.0.0", false},
		{"empty", "", "", "", "", true},
		{"no slash", "invalid", "", "", "", true},
		{"empty version after colon", "ns/slug:", "", "", "", true},
		{"empty namespace", "/slug", "", "", "", true},
		{"empty slug", "ns/", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ns, slug, ver, err := ParseBundleRef(tt.raw)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if ns != tt.wantNS {
				t.Fatalf("namespace = %q, want %q", ns, tt.wantNS)
			}

			if slug != tt.wantSlug {
				t.Fatalf("slug = %q, want %q", slug, tt.wantSlug)
			}

			if ver != tt.wantVer {
				t.Fatalf("version = %q, want %q", ver, tt.wantVer)
			}
		})
	}
}
