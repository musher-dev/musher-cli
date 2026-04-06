package workflow

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/bundle/session"
	"github.com/musher-dev/musher-cli/internal/harness"
)

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

func TestBuildHarnessArgs_WithBundleDir(t *testing.T) {
	t.Parallel()

	ls := &session.LoadSession{
		BundleDir:  "/tmp/bundle",
		WorkingDir: "/tmp/project",
	}

	spec := &harness.Spec{
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
	}

	args := BuildHarnessArgs(ls, spec)

	if len(args) < 2 {
		t.Fatalf("expected at least 2 args, got %d", len(args))
	}

	if args[0] != "--add-dir" {
		t.Errorf("args[0] = %q, want %q", args[0], "--add-dir")
	}

	if args[1] != "/tmp/bundle" {
		t.Errorf("args[1] = %q, want %q", args[1], "/tmp/bundle")
	}
}

func TestBuildHarnessArgs_SameDir(t *testing.T) {
	t.Parallel()

	ls := &session.LoadSession{
		BundleDir:  "/tmp/project",
		WorkingDir: "/tmp/project",
	}

	spec := &harness.Spec{
		BundleDir: harness.BundleDirSpec{
			Mode: "cwd",
		},
	}

	args := BuildHarnessArgs(ls, spec)

	if len(args) != 0 {
		t.Fatalf("expected empty args for same dir, got %v", args)
	}
}

func TestLoadManifestFromCache_Success(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Write a blob and manifest so LoadManifest succeeds.
	content := []byte("test content")

	digest, blobErr := store.StoreBlob(content)
	if blobErr != nil {
		t.Fatalf("StoreBlob: %v", blobErr)
	}

	manifest := &cache.BundleManifest{
		Namespace:   "testns",
		Slug:        "testslug",
		Version:     "1.0.0",
		Name:        "Test Bundle",
		Description: "A test bundle",
		Layers: []cache.ManifestLayer{
			{
				LogicalPath:   "skills/test/SKILL.md",
				AssetType:     "skill",
				ContentSHA256: digest,
				Size:          int64(len(content)),
			},
		},
	}

	if writeErr := store.StoreManifest("host1", "testns", "testslug", "1.0.0", manifest); writeErr != nil {
		t.Fatalf("StoreManifest: %v", writeErr)
	}

	result := &pull.Result{
		Namespace: "testns",
		Slug:      "testslug",
		Version:   "1.0.0",
		CacheRoot: cacheRoot,
		HostID:    "host1",
	}

	gotStore, gotManifest, gotErr := LoadManifestFromCache(result)
	if gotErr != nil {
		t.Fatalf("LoadManifestFromCache: %v", gotErr)
	}

	if gotStore == nil {
		t.Fatal("store is nil")
	}

	if gotManifest.Name != "Test Bundle" {
		t.Errorf("manifest name = %q, want %q", gotManifest.Name, "Test Bundle")
	}
}

func TestLoadManifestFromCache_MissingManifest(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	// Initialize store but don't write any manifests.
	if _, err := cache.NewStore(cacheRoot); err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	result := &pull.Result{
		Namespace: "testns",
		Slug:      "testslug",
		Version:   "1.0.0",
		CacheRoot: cacheRoot,
		HostID:    "host1",
	}

	_, _, err := LoadManifestFromCache(result)
	if err == nil {
		t.Fatal("expected error for missing manifest, got nil")
	}
}
