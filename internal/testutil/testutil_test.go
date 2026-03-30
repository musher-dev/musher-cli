package testutil_test

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/testutil"
)

func TestNewMockClient(t *testing.T) {
	t.Parallel()

	var gotPath string

	c := testutil.NewMockClient("test-key", func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		return testutil.JSONResponse(http.StatusOK, `{"ok":true}`), nil
	})

	if !c.IsAuthenticated() {
		t.Fatal("expected authenticated client")
	}

	// Verify the base URL was set correctly.
	if got := c.BaseURL(); got != "https://api.test" {
		t.Errorf("BaseURL = %q, want %q", got, "https://api.test")
	}

	_ = gotPath // used by transport function
}

func TestJSONResponse(t *testing.T) {
	t.Parallel()

	resp := testutil.JSONResponse(http.StatusNotFound, `{"error":"not found"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestWriteFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := testutil.WriteFile(t, dir, "sub/test.txt", "hello")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", string(got), "hello")
	}
}

func TestOverridePaths(t *testing.T) {
	root := t.TempDir()
	testutil.OverridePaths(t, root)

	if got := os.Getenv("MUSHER_CONFIG_HOME"); got != filepath.Join(root, "config") {
		t.Errorf("MUSHER_CONFIG_HOME = %q, want %q", got, filepath.Join(root, "config"))
	}

	if got := os.Getenv("MUSHER_CACHE_HOME"); got != filepath.Join(root, "cache") {
		t.Errorf("MUSHER_CACHE_HOME = %q, want %q", got, filepath.Join(root, "cache"))
	}
}

func TestTestContext(t *testing.T) {
	ctx, stdout, _ := testutil.TestContext(t)

	out := output.FromContext(ctx)
	out.Println("test output")

	if got := stdout.String(); got != "test output\n" {
		t.Errorf("stdout = %q, want %q", got, "test output\n")
	}
}

func TestNewTestServer(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listeners unavailable in this environment: %v", err)
	}

	_ = listener.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv, c := testutil.NewTestServer(t, handler, "test-key")

	if srv.URL == "" {
		t.Fatal("server URL is empty")
	}

	if !c.IsAuthenticated() {
		t.Fatal("expected authenticated client")
	}
}
