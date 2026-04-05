package canary

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

var (
	buildOnce    sync.Once
	buildPath    string
	errBuildFail error
)

func TestGoldenPathCanary(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_KEY"))
	bundleRef := strings.TrimSpace(os.Getenv("MUSHER_CANARY_PUBLIC_BUNDLE_REF"))
	expectedNamespace := strings.TrimSpace(os.Getenv("MUSHER_CANARY_EXPECTED_NAMESPACE"))

	if baseURL == "" || apiKey == "" || bundleRef == "" || expectedNamespace == "" {
		t.Skip("canary disabled; set MUSHER_CANARY_API_URL, MUSHER_CANARY_API_KEY, MUSHER_CANARY_PUBLIC_BUNDLE_REF, and MUSHER_CANARY_EXPECTED_NAMESPACE")
	}

	namespace, slug, version, err := parseBundleRef(bundleRef)
	if err != nil {
		t.Fatalf("parse MUSHER_CANARY_PUBLIC_BUNDLE_REF: %v", err)
	}

	httpClient, err := client.NewInstrumentedHTTPClient(strings.TrimSpace(os.Getenv("MUSHER_NETWORK_CA_CERT_FILE")))
	if err != nil {
		t.Fatalf("build HTTP client: %v", err)
	}

	c := client.NewWithHTTPClient(baseURL, apiKey, httpClient)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	if _, validateErr := c.ValidateKey(ctx); validateErr != nil {
		t.Fatalf("validate key: %v", validateErr)
	}

	identity, err := c.GetPublisherIdentity(ctx)
	if err != nil {
		t.Fatalf("get publisher identity: %v", err)
	}

	if !hasNamespace(identity.Namespaces, expectedNamespace) {
		t.Fatalf("expected namespace %q not present in publisher identity", expectedNamespace)
	}

	searchJSON := runCLI(t, baseURL, apiKey, "hub", "search", slug, "--limit", "20", "--json")

	var searchResult client.HubSearchResponse

	if unmarshalErr := json.Unmarshal([]byte(searchJSON), &searchResult); unmarshalErr != nil {
		t.Fatalf("decode hub search JSON: %v\noutput: %s", unmarshalErr, searchJSON)
	}

	if !containsBundle(searchResult.Data, namespace, slug) {
		t.Fatalf("hub search results do not contain %s/%s", namespace, slug)
	}

	tmpDir := t.TempDir()
	runCLI(t, baseURL, apiKey, "bundle", "pull", bundleRef, "--output-dir", tmpDir, "--json")

	pullResult, err := c.PullPublicBundleVersion(ctx, namespace, slug, version)
	if err != nil {
		t.Fatalf("pull public bundle version via client: %v", err)
	}

	if len(pullResult.Assets) == 0 {
		t.Fatalf("public bundle %s has no assets", bundleRef)
	}

	for i := range pullResult.Assets {
		asset := &pullResult.Assets[i]
		path := filepath.Join(tmpDir, asset.LogicalPath)

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("expected pulled asset %s: %v", asset.LogicalPath, err)
		}

		if info.Size() == 0 {
			t.Fatalf("pulled asset %s is empty", asset.LogicalPath)
		}
	}
}

func TestCanaryHealthProbeBudget(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_URL"))
	if baseURL == "" {
		t.Skip("canary disabled; set MUSHER_CANARY_API_URL")
	}

	start := time.Now()

	result := client.ProbeHealth(t.Context(), baseURL, strings.TrimSpace(os.Getenv("MUSHER_NETWORK_CA_CERT_FILE")))
	if !result.Reachable {
		t.Fatalf("health probe unreachable: %s", result.Error)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("health probe exceeded budget: %s", elapsed)
	}
}

func parseBundleRef(ref string) (namespace, slug, version string, err error) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", "", "", errors.New("expected namespace/slug:version")
	}

	nsParts := strings.SplitN(parts[0], "/", 2)
	if len(nsParts) != 2 || nsParts[0] == "" || nsParts[1] == "" {
		return "", "", "", errors.New("expected namespace/slug:version")
	}

	return nsParts[0], nsParts[1], parts[1], nil
}

func hasNamespace(namespaces []client.NamespaceHandle, want string) bool {
	for _, ns := range namespaces {
		if ns.Handle == want {
			return true
		}
	}

	return false
}

func containsBundle(results []client.HubBundleSummary, namespace, slug string) bool {
	for i := range results {
		if results[i].Publisher.Handle == namespace && results[i].Slug == slug {
			return true
		}
	}

	return false
}

func runCLI(t *testing.T, baseURL, apiKey string, args ...string) string {
	t.Helper()

	binPath := buildCLI(t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	cmdArgs := append([]string{"--no-color", "--no-input", "--api-url", baseURL}, args...)
	cmd := exec.CommandContext(ctx, binPath, cmdArgs...)

	cmd.Env = append(os.Environ(), "MUSHER_API_KEY="+apiKey)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %q: %v\n%s", strings.Join(cmdArgs, " "), err, output)
	}

	return string(output)
}

func buildCLI(t *testing.T) string {
	t.Helper()

	buildOnce.Do(func() {
		exeSuffix := ""
		if runtime.GOOS == "windows" {
			exeSuffix = ".exe"
		}

		root, err := repoRoot()
		if err != nil {
			errBuildFail = err
			return
		}

		dir, err := os.MkdirTemp("", "musher-canary-bin-") //nolint:usetesting // sync.Once outlives individual test instances
		if err != nil {
			errBuildFail = repoerrors.Errorf("create canary bin dir: %w", err)
			return
		}

		buildPath = filepath.Join(dir, "musher"+exeSuffix)

		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", buildPath, "./cmd/musher") //nolint:usetesting // sync.Once outlives individual test instances
		cmd.Dir = root

		cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(os.TempDir(), "musher-canary-gocache")) //nolint:usetesting // sync.Once outlives individual test instances

		output, err := cmd.CombinedOutput()
		if err != nil {
			errBuildFail = repoerrors.Errorf("build musher canary binary: %w\n%s", err, output)
		}
	})

	if errBuildFail != nil {
		t.Fatalf("build CLI: %v", errBuildFail)
	}

	return buildPath
}

func repoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve test file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..")), nil
}

// ---------------------------------------------------------------------------
// Auth status canary
// ---------------------------------------------------------------------------

type canaryAuthStatus struct {
	Authenticated bool `json:"authenticated"`
	Namespaces    []struct {
		Handle string `json:"handle"`
	} `json:"namespaces"`
}

func TestCanaryAuthStatus(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_KEY"))
	expectedNamespace := strings.TrimSpace(os.Getenv("MUSHER_CANARY_EXPECTED_NAMESPACE"))

	if baseURL == "" || apiKey == "" || expectedNamespace == "" {
		t.Skip("canary disabled; set MUSHER_CANARY_API_URL, MUSHER_CANARY_API_KEY, and MUSHER_CANARY_EXPECTED_NAMESPACE")
	}

	out := runCLI(t, baseURL, apiKey, "auth", "status", "--json")

	var status canaryAuthStatus
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatalf("decode auth status JSON: %v\noutput: %s", err, out)
	}

	if !status.Authenticated {
		t.Fatal("auth status reports not authenticated")
	}

	found := false

	for _, ns := range status.Namespaces {
		if ns.Handle == expectedNamespace {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected namespace %q not found in auth status namespaces", expectedNamespace)
	}
}

// ---------------------------------------------------------------------------
// Bundle load canary
// ---------------------------------------------------------------------------

type canaryBundleLoadResult struct {
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Version    string `json:"version"`
	AssetCount int    `json:"assetCount"`
	CacheRoot  string `json:"cacheRoot"`
}

func TestCanaryBundleLoad(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_KEY"))
	bundleRef := strings.TrimSpace(os.Getenv("MUSHER_CANARY_PUBLIC_BUNDLE_REF"))

	if baseURL == "" || apiKey == "" || bundleRef == "" {
		t.Skip("canary disabled; set MUSHER_CANARY_API_URL, MUSHER_CANARY_API_KEY, and MUSHER_CANARY_PUBLIC_BUNDLE_REF")
	}

	namespace, slug, _, err := parseBundleRef(bundleRef)
	if err != nil {
		t.Fatalf("parse MUSHER_CANARY_PUBLIC_BUNDLE_REF: %v", err)
	}

	out := runCLI(t, baseURL, apiKey, "bundle", "load", bundleRef, "--json")

	var result canaryBundleLoadResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode bundle load JSON: %v\noutput: %s", err, out)
	}

	if result.Namespace != namespace {
		t.Fatalf("expected namespace %q, got %q", namespace, result.Namespace)
	}

	if result.Slug != slug {
		t.Fatalf("expected slug %q, got %q", slug, result.Slug)
	}

	if result.AssetCount == 0 {
		t.Fatal("bundle load returned zero assets")
	}

	if result.CacheRoot == "" {
		t.Fatal("bundle load returned empty cacheRoot")
	}
}

// ---------------------------------------------------------------------------
// Bundle push + yank canary
// ---------------------------------------------------------------------------

func TestCanaryBundlePushAndYank(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_KEY"))
	pushNamespace := strings.TrimSpace(os.Getenv("MUSHER_CANARY_PUSH_NAMESPACE"))
	pushSlug := strings.TrimSpace(os.Getenv("MUSHER_CANARY_PUSH_SLUG"))

	if baseURL == "" || apiKey == "" || pushNamespace == "" || pushSlug == "" {
		t.Skip("canary disabled; set MUSHER_CANARY_API_URL, MUSHER_CANARY_API_KEY, MUSHER_CANARY_PUSH_NAMESPACE, and MUSHER_CANARY_PUSH_SLUG")
	}

	version := canaryVersion()
	bundleRef := pushNamespace + "/" + pushSlug + ":" + version
	dir := t.TempDir()

	scaffoldBundle(t, dir, pushNamespace, pushSlug, version, "private", false)

	// Push.
	runCLIInDir(t, baseURL, apiKey, dir, "bundle", "push", "--yes")

	// Register best-effort cleanup.
	t.Cleanup(func() {
		yankCLI(t, baseURL, apiKey, bundleRef)
	})

	// Verify round-trip via pull.
	pullDir := t.TempDir()
	runCLI(t, baseURL, apiKey, "bundle", "pull", bundleRef, "--output-dir", pullDir)

	promptPath := filepath.Join(pullDir, "prompt.md")

	info, err := os.Stat(promptPath)
	if err != nil {
		t.Fatalf("expected pulled asset prompt.md: %v", err)
	}

	if info.Size() == 0 {
		t.Fatal("pulled asset prompt.md is empty")
	}

	// Yank (also asserts yank works).
	runCLI(t, baseURL, apiKey, "bundle", "yank", bundleRef, "--yes", "--reason", "canary test cleanup")
}

// ---------------------------------------------------------------------------
// Hub publish + deprecate canary
// ---------------------------------------------------------------------------

func TestCanaryHubPublishAndDeprecate(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("MUSHER_CANARY_API_KEY"))
	pushNamespace := strings.TrimSpace(os.Getenv("MUSHER_CANARY_PUSH_NAMESPACE"))
	pushSlug := strings.TrimSpace(os.Getenv("MUSHER_CANARY_PUSH_SLUG"))

	if baseURL == "" || apiKey == "" || pushNamespace == "" || pushSlug == "" {
		t.Skip("canary disabled; set MUSHER_CANARY_API_URL, MUSHER_CANARY_API_KEY, MUSHER_CANARY_PUSH_NAMESPACE, and MUSHER_CANARY_PUSH_SLUG")
	}

	version := canaryVersion()
	bundleRef := pushNamespace + "/" + pushSlug + ":" + version
	hubRef := pushNamespace + "/" + pushSlug
	dir := t.TempDir()

	scaffoldBundle(t, dir, pushNamespace, pushSlug, version, "public", true)

	// Push the public bundle.
	runCLIInDir(t, baseURL, apiKey, dir, "bundle", "push", "--yes")

	// Register best-effort cleanup (deprecate then yank).
	t.Cleanup(func() {
		deprecateCLI(t, baseURL, apiKey, hubRef)
		yankCLI(t, baseURL, apiKey, bundleRef)
	})

	// Publish to Hub.
	runCLI(t, baseURL, apiKey, "hub", "publish", hubRef)

	// Verify the listing appears in search (retry for eventual consistency).
	var found bool

	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		searchJSON := runCLI(t, baseURL, apiKey, "hub", "search", pushSlug, "--limit", "20", "--json")

		var searchResult client.HubSearchResponse
		if err := json.Unmarshal([]byte(searchJSON), &searchResult); err != nil {
			t.Fatalf("decode hub search JSON: %v\noutput: %s", err, searchJSON)
		}

		if containsBundle(searchResult.Data, pushNamespace, pushSlug) {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("hub search did not return %s after publish", hubRef)
	}

	// Deprecate.
	runCLI(t, baseURL, apiKey, "hub", "deprecate", hubRef, "--message", "canary test cleanup")

	// Yank.
	runCLI(t, baseURL, apiKey, "bundle", "yank", bundleRef, "--yes", "--reason", "canary test cleanup")
}

// ---------------------------------------------------------------------------
// Additional helpers
// ---------------------------------------------------------------------------

// runCLIInDir is like runCLI but executes the binary in the given directory.
func runCLIInDir(t *testing.T, baseURL, apiKey, dir string, args ...string) string {
	t.Helper()

	binPath := buildCLI(t)

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	cmdArgs := append([]string{"--no-color", "--no-input", "--api-url", baseURL}, args...)
	cmd := exec.CommandContext(ctx, binPath, cmdArgs...)

	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "MUSHER_API_KEY="+apiKey)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run %q (dir=%s): %v\n%s", strings.Join(cmdArgs, " "), dir, err, output)
	}

	return string(output)
}

// canaryVersion returns a unique semver patch version based on the current unix timestamp.
func canaryVersion() string {
	return fmt.Sprintf("0.0.%d", time.Now().Unix())
}

// scaffoldBundle writes a minimal musher.yaml and asset files into dir.
// When hubReady is true it adds the fields required for Hub publishing.
func scaffoldBundle(t *testing.T, dir, namespace, slug, version, visibility string, hubReady bool) {
	t.Helper()

	yaml := fmt.Sprintf(`name: Canary Test Bundle
description: Automated canary test — safe to ignore
namespace: %s
slug: %s
version: %s
visibility: %s
assets:
  - id: canary-prompt
    src: prompt.md
    kind: prompt
`, namespace, slug, version, visibility)

	if hubReady {
		yaml += `readme: README.md
license: MIT
`
	}

	writeFile(t, dir, "musher.yaml", yaml)
	writeFile(t, dir, "prompt.md", "Canary test prompt — generated by golden path canary tests.\n")

	if hubReady {
		writeFile(t, dir, "README.md", "# Canary Test Bundle\n\nAutomated canary test bundle. Safe to ignore.\n")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// yankCLI attempts to yank a bundle version. Failures are logged, not fatal,
// so cleanup does not mask the original test failure.
func yankCLI(t *testing.T, baseURL, apiKey, bundleRef string) {
	t.Helper()

	binPath := buildCLI(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:usetesting // cleanup may run after test context is canceled
	defer cancel()

	cmdArgs := []string{"--no-color", "--no-input", "--api-url", baseURL, "bundle", "yank", bundleRef, "--yes", "--reason", "canary cleanup"}
	cmd := exec.CommandContext(ctx, binPath, cmdArgs...)

	cmd.Env = append(os.Environ(), "MUSHER_API_KEY="+apiKey)

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Logf("cleanup yank %s failed (best-effort): %v\n%s", bundleRef, err, output)
	}
}

// deprecateCLI attempts to deprecate a hub listing. Failures are logged, not fatal.
func deprecateCLI(t *testing.T, baseURL, apiKey, hubRef string) {
	t.Helper()

	binPath := buildCLI(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) //nolint:usetesting // cleanup may run after test context is canceled
	defer cancel()

	cmdArgs := []string{"--no-color", "--no-input", "--api-url", baseURL, "hub", "deprecate", hubRef, "--message", "canary cleanup"}
	cmd := exec.CommandContext(ctx, binPath, cmdArgs...)

	cmd.Env = append(os.Environ(), "MUSHER_API_KEY="+apiKey)

	if output, err := cmd.CombinedOutput(); err != nil {
		t.Logf("cleanup deprecate %s failed (best-effort): %v\n%s", hubRef, err, output)
	}
}
