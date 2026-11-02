package detect

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func touch(t *testing.T, root string, files ...string) {
	t.Helper()

	for _, file := range files {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectRootDockerfile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	touch(t, root, "Dockerfile", "package.json", "requirements.txt", "pyproject.toml")

	det, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(det.Dockerfiles, []string{"Dockerfile"}) || det.Ambiguous() {
		t.Errorf("Dockerfiles = %v", det.Dockerfiles)
	}

	if det.Suggested.Dockerfile != "Dockerfile" || det.Suggested.Buildpacks != nil || det.Suggested.Context != "." {
		t.Errorf("Suggested = %+v", det.Suggested)
	}

	if !slices.Equal(det.LanguageHints, []string{"node", "python"}) {
		t.Errorf("LanguageHints = %v", det.LanguageHints)
	}
}

func TestDetectOrdersCandidates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	touch(t, root,
		"docker/worker.Dockerfile", "docker/api.Dockerfile", "docker/Dockerfile",
		"web.Dockerfile", "Dockerfile", ".Dockerfile", "Dockerfile.dev",
	)

	if err := os.Mkdir(filepath.Join(root, "dir.Dockerfile"), 0o755); err != nil {
		t.Fatal(err)
	}

	det, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Dockerfile", "docker/Dockerfile", "web.Dockerfile", "docker/api.Dockerfile", "docker/worker.Dockerfile"}
	if !slices.Equal(det.Dockerfiles, want) {
		t.Errorf("Dockerfiles = %v, want %v", det.Dockerfiles, want)
	}

	if !det.Ambiguous() {
		t.Error("several candidates should be ambiguous")
	}
}

func TestDetectTwoSuffixDockerfiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	touch(t, root, "docker/web.Dockerfile", "docker/api.Dockerfile", "go.mod")

	det, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(det.Dockerfiles, []string{"docker/api.Dockerfile", "docker/web.Dockerfile"}) || !det.Ambiguous() {
		t.Errorf("Dockerfiles = %v", det.Dockerfiles)
	}

	if det.Suggested.Dockerfile != "docker/api.Dockerfile" {
		t.Errorf("Suggested.Dockerfile = %q", det.Suggested.Dockerfile)
	}

	if !slices.Equal(det.LanguageHints, []string{"go"}) {
		t.Errorf("LanguageHints = %v", det.LanguageHints)
	}
}

func TestDetectNoDockerfileSuggestsPaketo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	touch(t, root, "Gemfile", "Procfile")

	det, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(det.Dockerfiles) != 0 || det.Ambiguous() {
		t.Errorf("Dockerfiles = %v", det.Dockerfiles)
	}

	if det.Suggested.Buildpacks == nil || det.Suggested.Buildpacks.BuilderImage != DefaultBuilder || det.Suggested.Dockerfile != "" {
		t.Errorf("Suggested = %+v", det.Suggested)
	}

	if !slices.Equal(det.LanguageHints, []string{"ruby", "procfile"}) {
		t.Errorf("LanguageHints = %v", det.LanguageHints)
	}
}

func TestDetectRejectsNonDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	touch(t, root, "file")

	if _, err := Detect(filepath.Join(root, "file")); err == nil {
		t.Error("a file is not a project directory")
	}

	if _, err := Detect(filepath.Join(root, "missing")); err == nil {
		t.Error("a missing directory should fail")
	}
}

func TestPinImage(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + strings.Repeat("c", 64)
	calls := 0
	digestOf := func(_ context.Context, ref string) (string, error) {
		calls++

		if strings.Contains(ref, "broken") {
			return "", errors.New("registry down")
		}

		return digest, nil
	}

	tests := map[string]string{
		"paketobuildpacks/builder-jammy-base":                    "paketobuildpacks/builder-jammy-base@" + digest,
		"paketobuildpacks/builder-jammy-base:0.4.1":              "paketobuildpacks/builder-jammy-base@" + digest,
		"localhost:5000/builder:latest":                          "localhost:5000/builder@" + digest,
		"ghcr.io/acme/builder@sha256:" + strings.Repeat("d", 64): "ghcr.io/acme/builder@sha256:" + strings.Repeat("d", 64),
	}

	for ref, want := range tests {
		got, err := pinImage(t.Context(), ref, digestOf)
		if err != nil || got != want {
			t.Errorf("pinImage(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}

	if calls != 3 {
		t.Errorf("registry consulted %d times, want 3 (a digest ref needs no lookup)", calls)
	}

	if _, err := pinImage(t.Context(), "acme/broken", digestOf); err == nil {
		t.Error("registry failure should propagate")
	}

	if _, err := pinImage(t.Context(), "Bad Ref", digestOf); err == nil {
		t.Error("invalid reference should fail")
	}

	// PinImage with an already-pinned ref never reaches the network.
	pinned := "acme/x@sha256:" + strings.Repeat("e", 64)
	if got, err := PinImage(t.Context(), pinned); err != nil || got != pinned {
		t.Errorf("PinImage(pinned) = %q, %v", got, err)
	}
}
