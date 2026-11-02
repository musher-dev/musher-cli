package build

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 1, 12, 34, 56, 0, time.FixedZone("EST", -5*3600))

const fixedStamp = "20261101173456"

func requireGit(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir

	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return strings.TrimSpace(string(out))
}

// newRepo creates a git repository with one commit and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)

	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	writeFile(t, filepath.Join(dir, "Dockerfile"), "FROM scratch\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "init")

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultTagCleanRepo(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	short := runGit(t, dir, "rev-parse", "--short", "HEAD")

	if got := DefaultTag(t.Context(), dir, fixedNow); got != short {
		t.Errorf("DefaultTag = %q, want %q", got, short)
	}
}

func TestDefaultTagDirtyRepo(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	short := runGit(t, dir, "rev-parse", "--short", "HEAD")
	writeFile(t, filepath.Join(dir, "untracked.txt"), "x")

	want := short + "-dirty." + fixedStamp
	if got := DefaultTag(t.Context(), dir, fixedNow); got != want {
		t.Errorf("DefaultTag = %q, want %q", got, want)
	}
}

func TestDefaultTagOutsideGit(t *testing.T) {
	t.Parallel()

	if got := DefaultTag(t.Context(), t.TempDir(), fixedNow); got != "build."+fixedStamp {
		t.Errorf("DefaultTag = %q, want build.%s", got, fixedStamp)
	}
}

func TestLabels(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	runGit(t, dir, "remote", "add", "origin", "https://user:tok3n@github.com/acme/web.git")
	revision := runGit(t, dir, "rev-parse", "HEAD")
	recipe := Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}

	labels := Labels(t.Context(), dir, recipe, fixedNow)

	want := map[string]string{
		LabelRevision:     revision,
		LabelSource:       "https://github.com/acme/web.git",
		LabelCreated:      "2026-11-01T17:34:56Z",
		LabelRecipeDigest: recipe.Digest(),
	}

	for key, value := range want {
		if labels[key] != value {
			t.Errorf("%s = %q, want %q", key, labels[key], value)
		}
	}

	outside := Labels(t.Context(), t.TempDir(), recipe, fixedNow)
	if _, ok := outside[LabelRevision]; ok {
		t.Error("revision label outside git")
	}

	if _, ok := outside[LabelSource]; ok {
		t.Error("source label outside git")
	}
}

func TestRedactRemote(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://user:pass@github.com/acme/web.git": "https://github.com/acme/web.git",
		"https://github.com/acme/web":               "https://github.com/acme/web",
		"git@github.com:acme/web.git":               "git@github.com:acme/web.git",
		"ssh://git@github.com/acme/web.git":         "ssh://github.com/acme/web.git",
		"/srv/git/web.git":                          "/srv/git/web.git",
	}

	for in, want := range tests {
		if got := redactRemote(in); got != want {
			t.Errorf("redactRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewRequestDefaults(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	recipe := Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}

	req, err := NewRequest(t.Context(), recipe, root, RequestOptions{
		Now:    func() time.Time { return fixedNow },
		Labels: map[string]string{"extra": "1", LabelCreated: "override"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if req.Tag != "build."+fixedStamp || req.Platform != DefaultPlatform || req.Root != root {
		t.Errorf("request = %+v", req)
	}

	if req.Labels["extra"] != "1" || req.Labels[LabelCreated] != "override" || req.Labels[LabelRecipeDigest] != recipe.Digest() {
		t.Errorf("labels = %v", req.Labels)
	}
}

func TestNewRequestOverridesAndValidation(t *testing.T) {
	t.Parallel()

	recipe := Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}

	req, err := NewRequest(t.Context(), recipe, ".", RequestOptions{Tag: "v1.2.3", Platform: " linux/arm64 "})
	if err != nil {
		t.Fatal(err)
	}

	if req.Tag != "v1.2.3" || req.Platform != "linux/arm64" || !filepath.IsAbs(req.Root) {
		t.Errorf("request = %+v", req)
	}

	if !regexp.MustCompile(`^\d{4}-`).MatchString(req.Labels[LabelCreated]) {
		t.Errorf("created label = %q", req.Labels[LabelCreated])
	}

	if _, err := NewRequest(t.Context(), recipe, ".", RequestOptions{Tag: "-bad"}); err == nil {
		t.Error("invalid tag accepted")
	}

	if _, err := NewRequest(t.Context(), recipe, ".", RequestOptions{Tag: "ok", Platform: "not/a/valid/platform"}); err == nil {
		t.Error("invalid platform accepted")
	}
}
