package canary

import (
	"context"
	"encoding/json"
	"errors"
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
