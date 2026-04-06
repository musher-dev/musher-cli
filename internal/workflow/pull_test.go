package workflow

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/client"
)

func TestResolveVersion_ExplicitVersion(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	deps := &PullDeps{
		Progress: NopProgressFactory{},
		HostID:   "host1",
	}

	resolved, fromLatest, resolveErr := resolveVersion(t.Context(), deps, store, "ns", "slug", "1.2.3", false)
	if resolveErr != nil {
		t.Fatalf("resolveVersion: %v", resolveErr)
	}

	if resolved != "1.2.3" {
		t.Errorf("resolved = %q, want %q", resolved, "1.2.3")
	}

	if fromLatest {
		t.Error("fromLatest should be false for explicit version")
	}
}

func TestResolveVersion_ExplicitVersionIgnoresForce(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	deps := &PullDeps{
		Progress: NopProgressFactory{},
		HostID:   "host1",
	}

	// Even with force=true, an explicit version should be returned immediately.
	resolved, fromLatest, resolveErr := resolveVersion(t.Context(), deps, store, "ns", "slug", "2.0.0", true)
	if resolveErr != nil {
		t.Fatalf("resolveVersion: %v", resolveErr)
	}

	if resolved != "2.0.0" {
		t.Errorf("resolved = %q, want %q", resolved, "2.0.0")
	}

	if fromLatest {
		t.Error("fromLatest should be false for explicit version")
	}
}

func TestPullBundleWithFallback_PublicClient(t *testing.T) {
	t.Parallel()

	bundleResp := &client.PullBundleResponse{
		Namespace:   "acme",
		Slug:        "test",
		Version:     "1.0.0",
		Name:        "Test Bundle",
		Description: "A test bundle",
		Assets: []client.PullBundleAsset{
			{
				LogicalPath: "skills/hello/SKILL.md",
				AssetType:   "skill",
				ContentText: "# Hello",
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundleResp)
	}))
	defer srv.Close()

	publicClient := client.New(srv.URL, "")
	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: publicClient,
	}

	bundle, err := pullBundleWithFallback(t.Context(), deps, publicClient, "acme", "test", "1.0.0", true)
	if err != nil {
		t.Fatalf("pullBundleWithFallback: %v", err)
	}

	if bundle.Name != "Test Bundle" {
		t.Errorf("name = %q, want %q", bundle.Name, "Test Bundle")
	}
}

func TestPullBundleWithFallback_AuthenticatedFallsBackToPublic(t *testing.T) {
	t.Parallel()

	bundleResp := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Public Fallback",
	}

	// Auth server returns 403.
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
	}))
	defer authSrv.Close()

	// Public server succeeds.
	pubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundleResp)
	}))
	defer pubSrv.Close()

	authClient := client.New(authSrv.URL, "key")
	publicClient := client.New(pubSrv.URL, "")

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: publicClient,
		AuthClient:   authClient,
	}

	bundle, err := pullBundleWithFallback(t.Context(), deps, authClient, "acme", "test", "1.0.0", false)
	if err != nil {
		t.Fatalf("pullBundleWithFallback: %v", err)
	}

	if bundle.Name != "Public Fallback" {
		t.Errorf("name = %q, want %q", bundle.Name, "Public Fallback")
	}
}

func TestCacheBundle_Success(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	bundle := &client.PullBundleResponse{
		Namespace:   "acme",
		Slug:        "test",
		Version:     "1.0.0",
		Name:        "Test",
		Description: "Test bundle",
		Assets: []client.PullBundleAsset{
			{
				LogicalPath: "skills/hello/SKILL.md",
				AssetType:   "skill",
				ContentText: "# Hello Skill",
			},
		},
	}

	manifest, cacheErr := cacheBundle(store, bundle)
	if cacheErr != nil {
		t.Fatalf("cacheBundle: %v", cacheErr)
	}

	if manifest.Namespace != "acme" {
		t.Errorf("namespace = %q, want %q", manifest.Namespace, "acme")
	}

	if len(manifest.Layers) != 1 {
		t.Fatalf("layers = %d, want 1", len(manifest.Layers))
	}

	if manifest.Layers[0].LogicalPath != "skills/hello/SKILL.md" {
		t.Errorf("layer path = %q", manifest.Layers[0].LogicalPath)
	}
}

func TestStoreManifestAndMeta_Success(t *testing.T) {
	t.Parallel()

	cacheRoot := t.TempDir()

	store, err := cache.NewStore(cacheRoot)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
		Layers:    []cache.ManifestLayer{},
	}

	storeErr := storeManifestAndMeta(store, "host1", "acme", "test", "1.0.0", manifest, false)
	if storeErr != nil {
		t.Fatalf("storeManifestAndMeta: %v", storeErr)
	}

	// Verify the manifest can be loaded back.
	loaded, loadErr := store.LoadManifest("host1", "acme", "test", "1.0.0")
	if loadErr != nil {
		t.Fatalf("LoadManifest: %v", loadErr)
	}

	if loaded.Name != "Test" {
		t.Errorf("name = %q, want %q", loaded.Name, "Test")
	}
}

func TestPullFromJSONAPI_PublicSuccess(t *testing.T) {
	t.Parallel()

	bundleResp := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundleResp)
	}))
	defer srv.Close()

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: client.New(srv.URL, ""),
		AuthClient:   nil, // no auth — forces public path
	}

	bundle, err := pullFromJSONAPI(t.Context(), deps, "acme", "test", "1.0.0")
	if err != nil {
		t.Fatalf("pullFromJSONAPI: %v", err)
	}

	if bundle.Name != "Test" {
		t.Errorf("name = %q, want %q", bundle.Name, "Test")
	}
}

func TestPullFromJSONAPI_AuthenticatedSuccess(t *testing.T) {
	t.Parallel()

	bundleResp := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "private-bundle",
		Version:   "2.0.0",
		Name:      "Private Bundle",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundleResp)
	}))
	defer srv.Close()

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: client.New(srv.URL, ""),
		AuthClient:   client.New(srv.URL, "key"),
	}

	bundle, err := pullFromJSONAPI(t.Context(), deps, "acme", "private-bundle", "2.0.0")
	if err != nil {
		t.Fatalf("pullFromJSONAPI: %v", err)
	}

	if bundle.Name != "Private Bundle" {
		t.Errorf("name = %q, want %q", bundle.Name, "Private Bundle")
	}
}

func TestResolveLatestVersion_Success(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"slug":          "test",
			"latestVersion": "3.2.1",
			"publisher":     map[string]string{"handle": "acme"},
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: client.New(srv.URL, ""),
	}

	version, err := resolveLatestVersion(t.Context(), deps, "acme", "test")
	if err != nil {
		t.Fatalf("resolveLatestVersion: %v", err)
	}

	if version != "3.2.1" {
		t.Errorf("version = %q, want %q", version, "3.2.1")
	}
}

func TestResolveLatestVersion_NoVersions(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"slug":          "test",
			"latestVersion": "",
			"publisher":     map[string]string{"handle": "acme"},
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: client.New(srv.URL, ""),
	}

	_, err := resolveLatestVersion(t.Context(), deps, "acme", "test")
	if err == nil {
		t.Fatal("expected error for no versions, got nil")
	}
}

func TestPullToCache_FullPipeline(t *testing.T) {
	t.Parallel()

	bundleResp := &client.PullBundleResponse{
		Namespace:   "acme",
		Slug:        "test",
		Version:     "1.0.0",
		Name:        "Test Bundle",
		Description: "A test bundle for pull",
		Assets: []client.PullBundleAsset{
			{
				LogicalPath: "skills/hello/SKILL.md",
				AssetType:   "skill",
				ContentText: "# Hello Skill\n\nTest content.",
			},
		},
	}

	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		// First request is resolve (for OCI), return 404 to skip OCI.
		// Second request is the JSON API pull.
		if requestCount == 1 {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})

			return
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundleResp)
	}))
	defer srv.Close()

	cacheRoot := t.TempDir()

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		CacheRoot:    cacheRoot,
		HostID:       "test-host",
		APIURL:       srv.URL,
		OCIRegistry:  "localhost:5000",
		AuthClient:   client.New(srv.URL, "test-key"),
		PublicClient: client.New(srv.URL, ""),
		APIKey:       "test-key",
	}

	result, err := PullToCache(t.Context(), deps, "acme", "test", "1.0.0", false)
	if err != nil {
		t.Fatalf("PullToCache: %v", err)
	}

	if result.Namespace != "acme" {
		t.Errorf("namespace = %q, want %q", result.Namespace, "acme")
	}

	if result.Slug != "test" {
		t.Errorf("slug = %q, want %q", result.Slug, "test")
	}

	if result.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", result.Version, "1.0.0")
	}

	if result.Cached {
		t.Error("expected Cached=false for fresh pull")
	}

	if len(result.Layers) != 1 {
		t.Fatalf("layers = %d, want 1", len(result.Layers))
	}

	// Second pull should be cached (no force).
	result2, err := PullToCache(t.Context(), deps, "acme", "test", "1.0.0", false)
	if err != nil {
		t.Fatalf("PullToCache (cached): %v", err)
	}

	if !result2.Cached {
		t.Error("expected Cached=true for second pull")
	}
}

func TestPullFromAPI_FallsBackToJSON(t *testing.T) {
	t.Parallel()

	bundleResp := &client.PullBundleResponse{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "JSON Fallback",
	}

	// First request (resolve) returns 404 to force OCI failure, second (pull) succeeds.
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount == 1 {
			// Resolve endpoint — fail to trigger JSON fallback.
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})

			return
		}

		// Pull endpoint — succeed.
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundleResp)
	}))
	defer srv.Close()

	deps := &PullDeps{
		Progress:     NopProgressFactory{},
		PublicClient: client.New(srv.URL, ""),
		AuthClient:   client.New(srv.URL, "key"),
		OCIRegistry:  "localhost:5000",
	}

	bundle, err := pullFromAPI(t.Context(), deps, "acme", "test", "1.0.0")
	if err != nil {
		t.Fatalf("pullFromAPI: %v", err)
	}

	if bundle.Name != "JSON Fallback" {
		t.Errorf("name = %q, want %q", bundle.Name, "JSON Fallback")
	}
}
