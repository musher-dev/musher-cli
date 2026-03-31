package bundledef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateSymlinkNotSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")

	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	errs := validateSymlink(info, path, dir, Asset{ID: "test", Src: "test.md"})
	if len(errs) != 0 {
		t.Errorf("expected no errors for non-symlink, got %v", errs)
	}
}

func TestValidateExtraFilesEmpty(t *testing.T) {
	t.Parallel()

	errs := validateExtraFiles(t.TempDir(), "", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for empty extra files, got %v", errs)
	}
}

func TestValidateExtraFilesReadmeExists(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644); err != nil {
		t.Fatal(err)
	}

	errs := validateExtraFiles(dir, "README.md", "")
	if len(errs) != 0 {
		t.Errorf("expected no errors for existing readme, got %v", errs)
	}
}

func TestValidateExtraFilesReadmeMissing(t *testing.T) {
	dir := t.TempDir()

	errs := validateExtraFiles(dir, "README.md", "")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for missing readme, got %d", len(errs))
	}
}

func TestValidateExtraFilesLicenseExists(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("MIT"), 0o644); err != nil {
		t.Fatal(err)
	}

	errs := validateExtraFiles(dir, "", "LICENSE")
	if len(errs) != 0 {
		t.Errorf("expected no errors for existing license, got %v", errs)
	}
}

func TestValidateExtraFilesLicenseMissing(t *testing.T) {
	dir := t.TempDir()

	errs := validateExtraFiles(dir, "", "LICENSE")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for missing license, got %d", len(errs))
	}
}

func TestValidateExtraFilesBothMissing(t *testing.T) {
	dir := t.TempDir()

	errs := validateExtraFiles(dir, "README.md", "LICENSE")
	if len(errs) != 2 {
		t.Fatalf("expected 2 errors for both missing, got %d: %v", len(errs), errs)
	}
}

func TestValidateAssetIDValid(t *testing.T) {
	t.Parallel()

	tests := []string{"review", "my-skill", "skill_v2", "a"}

	for _, id := range tests {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			seen := make(map[string]bool)

			errs := validateAssetID(0, id, seen)
			if len(errs) != 0 {
				t.Errorf("validateAssetID(%q) errors = %v, want none", id, errs)
			}
		})
	}
}

func TestValidateAssetIDEmpty(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)

	errs := validateAssetID(0, "", seen)
	if len(errs) == 0 {
		t.Error("expected error for empty asset ID")
	}
}

func TestValidateAssetIDDuplicate(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{"review": true}

	errs := validateAssetID(1, "review", seen)
	if len(errs) == 0 {
		t.Error("expected error for duplicate asset ID")
	}
}

func TestValidateAssetSrcEmpty(t *testing.T) {
	t.Parallel()

	errs := validateAssetSrc(0, "")
	if len(errs) == 0 {
		t.Error("expected error for empty asset src")
	}
}

func TestValidateAssetSrcValid(t *testing.T) {
	t.Parallel()

	errs := validateAssetSrc(0, "skills/review/SKILL.md")
	if len(errs) != 0 {
		t.Errorf("expected no errors for valid src, got %v", errs)
	}
}
