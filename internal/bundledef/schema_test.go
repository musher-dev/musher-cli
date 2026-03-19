package bundledef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSchemaValidFixtures(t *testing.T) {
	t.Parallel()

	fixtures, err := filepath.Glob("testdata/valid/*.yaml")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	if len(fixtures) == 0 {
		t.Fatal("no valid fixtures found")
	}

	for _, f := range fixtures {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			errs := ValidateSchema(data)
			if len(errs) > 0 {
				t.Errorf("expected no errors for %s, got:", f)
				for _, e := range errs {
					t.Errorf("  %s", e)
				}
			}
		})
	}
}

func TestValidateSchemaInvalidFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []struct {
		file    string
		wantErr string
	}{
		{"testdata/invalid/missing-namespace.yaml", "namespace"},
		{"testdata/invalid/missing-assets.yaml", "assets"},
		{"testdata/invalid/unknown-field.yaml", "bogusField"},
		{"testdata/invalid/bad-visibility.yaml", "visibility"},
	}

	for _, tt := range fixtures {
		t.Run(filepath.Base(tt.file), func(t *testing.T) {
			t.Parallel()

			data, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read: %v", err)
			}

			errs := ValidateSchema(data)
			if len(errs) == 0 {
				t.Fatalf("expected errors for %s, got none", tt.file)
			}

			found := false
			for _, e := range errs {
				if contains(e.String(), tt.wantErr) {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("expected error containing %q, got:", tt.wantErr)
				for _, e := range errs {
					t.Errorf("  %s", e)
				}
			}
		})
	}
}

func TestValidateSchemaRejectsInvalidYAML(t *testing.T) {
	t.Parallel()

	errs := ValidateSchema([]byte("{{invalid yaml"))
	if len(errs) == 0 {
		t.Fatal("expected error for invalid YAML")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
