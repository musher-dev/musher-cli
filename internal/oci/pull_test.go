package oci_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/oci"
)

// testLayer holds the data for a single test layer.
type testLayer struct {
	logicalPath string
	assetType   string
	content     string
}

// testRegistry is a minimal OCI Distribution Spec v2 test server.
type testRegistry struct {
	layers      []testLayer
	tokenStatus int            // status for token endpoint; 0 means 200
	manifestErr int            // non-zero to return this status for manifest requests
	blobErrs    map[string]int // digest → status for blob requests
}

func (tr *testRegistry) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Token endpoint.
		if path == "/v2/token" {
			tr.handleToken(w, r)

			return
		}

		// OCI v2 discovery.
		if path == "/v2/" {
			tr.handleV2Check(w, r)

			return
		}

		// Manifest endpoints: /v2/<repo>/manifests/<ref>
		if strings.Contains(path, "/manifests/") {
			tr.handleManifest(w, r)

			return
		}

		// Blob endpoints: /v2/<repo>/blobs/<digest>
		if strings.Contains(path, "/blobs/") {
			tr.handleBlob(w, r)

			return
		}

		w.WriteHeader(http.StatusNotFound)
	})
}

func (tr *testRegistry) handleV2Check(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(
			`Bearer realm="%s/v2/token",service="test-registry"`,
			"http://"+r.Host,
		))
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)

		return
	}

	w.WriteHeader(http.StatusOK)
}

func (tr *testRegistry) handleToken(w http.ResponseWriter, _ *http.Request) {
	status := tr.tokenStatus
	if status == 0 {
		status = http.StatusOK
	}

	if status != http.StatusOK {
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"errors":[{"code":"UNSUPPORTED","message":"internal error"}]}`)

		return
	}

	w.Header().Set("Content-Type", "application/json")

	payload := struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}{
		Token:     "test-token-12345",
		ExpiresIn: 300,
	}

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (tr *testRegistry) handleManifest(w http.ResponseWriter, r *http.Request) {
	if tr.manifestErr != 0 {
		w.WriteHeader(tr.manifestErr)
		fmt.Fprintf(w, `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"not found"}]}`)

		return
	}

	manifest := tr.buildManifest()

	data, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	dgst := sha256.Sum256(data)

	w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
	w.Header().Set("Docker-Content-Digest", "sha256:"+hex.EncodeToString(dgst[:]))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)

		return
	}

	_, _ = w.Write(data)
}

func (tr *testRegistry) handleBlob(w http.ResponseWriter, r *http.Request) {
	// Extract digest from path: /v2/<repo>/blobs/<digest>
	parts := strings.Split(r.URL.Path, "/blobs/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	digest := parts[len(parts)-1]

	if status, ok := tr.blobErrs[digest]; ok {
		w.WriteHeader(status)

		return
	}

	for _, l := range tr.layers {
		if layerDigest(l.content) == digest {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.Itoa(len(l.content)))

			_, _ = w.Write([]byte(l.content))

			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `{"errors":[{"code":"BLOB_UNKNOWN","message":"blob not found"}]}`)
}

func (tr *testRegistry) buildManifest() ocispec.Manifest {
	var layers []ocispec.Descriptor

	for _, l := range tr.layers {
		h := sha256.Sum256([]byte(l.content))
		layers = append(layers, ocispec.Descriptor{
			MediaType: "application/octet-stream",
			Digest:    godigest.NewDigestFromEncoded(godigest.SHA256, hex.EncodeToString(h[:])),
			Size:      int64(len(l.content)),
		})
	}

	return ocispec.Manifest{
		MediaType: ocispec.MediaTypeImageManifest,
		Layers:    layers,
	}
}

func layerDigest(content string) string {
	h := sha256.Sum256([]byte(content))

	return "sha256:" + hex.EncodeToString(h[:])
}

func layerDigestString(content string) string {
	h := sha256.Sum256([]byte(content))

	return hex.EncodeToString(h[:])
}

func buildResolveResponse(ts *httptest.Server, layers []testLayer) *client.ResolveResponse {
	host := strings.TrimPrefix(ts.URL, "http://")

	var resolveLayers []client.ResolveLayer
	for _, l := range layers {
		resolveLayers = append(resolveLayers, client.ResolveLayer{
			LogicalPath:   l.logicalPath,
			AssetType:     l.assetType,
			ContentSHA256: layerDigestString(l.content),
			SizeBytes:     int64(len(l.content)),
		})
	}

	return &client.ResolveResponse{
		Namespace:   "test-ns",
		Slug:        "test-bundle",
		Version:     "1.0.0",
		Name:        "Test Bundle",
		Description: "A test bundle",
		OCIRef:      host + "/test-ns/test-bundle:1.0.0",
		OCIDigest:   "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Manifest:    client.ResolveManifest{Layers: resolveLayers},
	}
}

func TestPullBundle_HappyPath(t *testing.T) {
	layers := []testLayer{
		{logicalPath: "skills/greeting/SKILL.md", assetType: "skill", content: "# Greeting Skill\nSay hello."},
		{logicalPath: "agents/helper.md", assetType: "agent_spec", content: "# Helper Agent\nAssist users."},
	}

	reg := &testRegistry{layers: layers}
	ts := httptest.NewServer(reg.handler())

	defer ts.Close()

	resolved := buildResolveResponse(ts, layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	result, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err != nil {
		t.Fatalf("PullBundle returned error: %v", err)
	}

	if result.Namespace != "test-ns" {
		t.Errorf("Namespace = %q, want %q", result.Namespace, "test-ns")
	}

	if result.Slug != "test-bundle" {
		t.Errorf("Slug = %q, want %q", result.Slug, "test-bundle")
	}

	if result.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", result.Version, "1.0.0")
	}

	if len(result.Assets) != 2 {
		t.Fatalf("got %d assets, want 2", len(result.Assets))
	}

	// Verify assets are correctly mapped.
	assetByPath := make(map[string]client.PullBundleAsset)
	for _, a := range result.Assets {
		assetByPath[a.LogicalPath] = a
	}

	skill, ok := assetByPath["skills/greeting/SKILL.md"]
	if !ok {
		t.Fatal("missing skills/greeting/SKILL.md asset")
	}

	if skill.AssetType != "skill" {
		t.Errorf("skill AssetType = %q, want %q", skill.AssetType, "skill")
	}

	if skill.ContentText != "# Greeting Skill\nSay hello." {
		t.Errorf("skill ContentText = %q, want %q", skill.ContentText, "# Greeting Skill\nSay hello.")
	}

	agent, ok := assetByPath["agents/helper.md"]
	if !ok {
		t.Fatal("missing agents/helper.md asset")
	}

	if agent.AssetType != "agent_spec" {
		t.Errorf("agent AssetType = %q, want %q", agent.AssetType, "agent_spec")
	}
}

func TestPullBundle_PublicNoCredentials(t *testing.T) {
	layers := []testLayer{
		{logicalPath: "skills/test/SKILL.md", assetType: "skill", content: "public content"},
	}

	reg := &testRegistry{layers: layers}
	ts := httptest.NewServer(reg.handler())

	defer ts.Close()

	resolved := buildResolveResponse(ts, layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		APIKey:      "", // no credentials
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	result, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err != nil {
		t.Fatalf("PullBundle returned error: %v", err)
	}

	if len(result.Assets) != 1 {
		t.Fatalf("got %d assets, want 1", len(result.Assets))
	}

	if result.Assets[0].ContentText != "public content" {
		t.Errorf("ContentText = %q, want %q", result.Assets[0].ContentText, "public content")
	}
}

func TestPullBundle_WithAPIKey(t *testing.T) {
	layers := []testLayer{
		{logicalPath: "skills/private/SKILL.md", assetType: "skill", content: "private content"},
	}

	var gotBasicAuth bool

	validToken := "valid-token-for-auth"

	// Build a registry that requires auth on all endpoints and checks
	// for basic auth credentials on the token endpoint.
	reg := &testRegistry{layers: layers}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Token endpoint — check for basic auth and issue token.
		if r.URL.Path == "/v2/token" {
			_, _, ok := r.BasicAuth()
			if ok {
				gotBasicAuth = true
			}

			w.Header().Set("Content-Type", "application/json")

			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      validToken,
				"expires_in": 300,
			})

			return
		}

		// All other endpoints require Bearer auth.
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer "+validToken {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/v2/token",service="test-registry"`,
				"http://"+r.Host,
			))
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)

			return
		}

		reg.handler().ServeHTTP(w, r)
	}))
	defer ts.Close()

	resolved := buildResolveResponse(ts, layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		APIKey:      "test-api-key",
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	result, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err != nil {
		t.Fatalf("PullBundle returned error: %v", err)
	}

	if !gotBasicAuth {
		t.Error("expected basic auth on token request when API key is provided")
	}

	if len(result.Assets) != 1 {
		t.Fatalf("got %d assets, want 1", len(result.Assets))
	}

	if result.Assets[0].ContentText != "private content" {
		t.Errorf("ContentText = %q, want %q", result.Assets[0].ContentText, "private content")
	}
}

func TestPullBundle_TokenEndpoint500(t *testing.T) {
	// When the token endpoint returns 500 and the registry requires auth
	// on manifest requests, the pull should fail.
	layers := []testLayer{{logicalPath: "test.md", assetType: "skill", content: "x"}}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/token" {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"errors":[{"code":"UNSUPPORTED","message":"internal error"}]}`)

			return
		}

		// All endpoints require auth — return 401 without valid bearer.
		authVal := r.Header.Get("Authorization")
		if authVal == "" || !strings.HasPrefix(authVal, "Bearer ") {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Bearer realm="%s/v2/token",service="test-registry"`,
				"http://"+r.Host,
			))
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resolved := buildResolveResponse(ts, layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	_, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err == nil {
		t.Fatal("expected error when token endpoint returns 500 and auth is required")
	}
}

func TestPullBundle_ManifestNotFound(t *testing.T) {
	reg := &testRegistry{
		layers:      []testLayer{{logicalPath: "test.md", assetType: "skill", content: "x"}},
		manifestErr: http.StatusNotFound,
	}

	ts := httptest.NewServer(reg.handler())
	defer ts.Close()

	resolved := buildResolveResponse(ts, reg.layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	_, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err == nil {
		t.Fatal("expected error when manifest returns 404")
	}
}

func TestPullBundle_BlobFetchFailure(t *testing.T) {
	layers := []testLayer{
		{logicalPath: "test.md", assetType: "skill", content: "blob content"},
	}

	reg := &testRegistry{
		layers: layers,
		blobErrs: map[string]int{
			layerDigest("blob content"): http.StatusInternalServerError,
		},
	}

	ts := httptest.NewServer(reg.handler())
	defer ts.Close()

	resolved := buildResolveResponse(ts, layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	_, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err == nil {
		t.Fatal("expected error when blob fetch returns 500")
	}
}

func TestPullBundle_EmptyOCIRef(t *testing.T) {
	resolved := &client.ResolveResponse{
		Namespace: "test",
		Slug:      "bundle",
		Version:   "1.0.0",
		OCIRef:    "",
	}

	cfg := oci.RegistryConfig{RegistryURL: "localhost"}

	_, err := oci.PullBundle(t.Context(), cfg, resolved)
	if err == nil {
		t.Fatal("expected error for empty OCI ref")
	}
}

func TestPullBundle_ContextCancellation(t *testing.T) {
	layers := []testLayer{
		{logicalPath: "test.md", assetType: "skill", content: "x"},
	}

	reg := &testRegistry{layers: layers}

	ts := httptest.NewServer(reg.handler())
	defer ts.Close()

	resolved := buildResolveResponse(ts, layers)
	cfg := oci.RegistryConfig{
		RegistryURL: strings.TrimPrefix(ts.URL, "http://"),
		HTTPClient:  ts.Client(),
		PlainHTTP:   true,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	_, err := oci.PullBundle(ctx, cfg, resolved)
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
}
