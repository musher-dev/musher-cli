package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeHealthReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := ProbeHealth(context.Background(), server.URL)

	if !result.Reachable {
		t.Errorf("expected reachable for test server, error: %s", result.Error)
	}

	if result.Host == "" {
		t.Error("host should not be empty")
	}

	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if result.Latency <= 0 {
		t.Error("latency should be positive")
	}
}

func TestProbeHealthUnreachable(t *testing.T) {
	// Use a non-routable address
	result := ProbeHealth(context.Background(), "http://192.0.2.1:1")

	if result.Reachable {
		t.Error("expected unreachable for non-routable address")
	}

	if result.Error == "" {
		t.Error("expected error message for unreachable host")
	}
}

func TestProbeHealthInvalidURL(t *testing.T) {
	result := ProbeHealth(context.Background(), "://invalid")

	if result.Reachable {
		t.Error("expected unreachable for invalid URL")
	}

	if result.Error == "" {
		t.Error("expected error for invalid URL")
	}
}

func TestProbeHealthEmptyHost(t *testing.T) {
	result := ProbeHealth(context.Background(), "http://")

	if result.Reachable {
		t.Error("expected unreachable for empty host")
	}
}

func TestProbeHealth4xxResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result := ProbeHealth(context.Background(), server.URL)

	// 4xx should still be considered reachable
	if !result.Reachable {
		t.Error("expected reachable for 4xx response")
	}

	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusNotFound)
	}
}

func TestProbeHealthWithDateHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result := ProbeHealth(context.Background(), server.URL)

	if !result.Reachable {
		t.Error("expected reachable")
	}

	if result.ServerTime == nil {
		t.Error("expected ServerTime to be parsed from Date header")
	}
}

func TestProbeHealthWithCACert(t *testing.T) {
	// Test with a non-existent CA cert file -- should still probe
	result := ProbeHealth(context.Background(), "http://192.0.2.1:1", "/nonexistent/ca.pem")

	if result.Reachable {
		t.Error("expected unreachable")
	}
}

func TestBuildProbeTLSConfigNonExistent(t *testing.T) {
	t.Parallel()

	_, err := buildProbeTLSConfig("/nonexistent/ca.pem")
	if err == nil {
		t.Fatal("expected error for non-existent CA file")
	}
}

func TestBuildProbeTLSConfigInvalidPEM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pem")

	if err := os.WriteFile(path, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := buildProbeTLSConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}
