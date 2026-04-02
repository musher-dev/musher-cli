package client

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeHealthReachable(t *testing.T) {
	result := probeHealthWithClient(t.Context(), "https://api.test", &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	})

	if !result.Reachable {
		t.Errorf("expected reachable for test server, error: %s", result.Error)
	}

	if result.Host == "" {
		t.Error("host should not be empty")
	}

	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if result.Latency < 0 {
		t.Error("latency should be non-negative")
	}
}

func TestProbeHealthUnreachable(t *testing.T) {
	// Use a non-routable address
	result := ProbeHealth(t.Context(), "http://192.0.2.1:1")

	if result.Reachable {
		t.Error("expected unreachable for non-routable address")
	}

	if result.Error == "" {
		t.Error("expected error message for unreachable host")
	}
}

func TestProbeHealthInvalidURL(t *testing.T) {
	result := ProbeHealth(t.Context(), "://invalid")

	if result.Reachable {
		t.Error("expected unreachable for invalid URL")
	}

	if result.Error == "" {
		t.Error("expected error for invalid URL")
	}
}

func TestProbeHealthEmptyHost(t *testing.T) {
	result := ProbeHealth(t.Context(), "http://")

	if result.Reachable {
		t.Error("expected unreachable for empty host")
	}
}

func TestProbeHealth4xxResponse(t *testing.T) {
	result := probeHealthWithClient(t.Context(), "https://api.test", &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	})

	// 4xx should still be considered reachable
	if !result.Reachable {
		t.Error("expected reachable for 4xx response")
	}

	if result.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusNotFound)
	}
}

func TestProbeHealthWithDateHeader(t *testing.T) {
	result := probeHealthWithClient(t.Context(), "https://api.test", &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			header := make(http.Header)
			header.Set("Date", "Mon, 02 Jan 2006 15:04:05 GMT")

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		}),
	})

	if !result.Reachable {
		t.Error("expected reachable")
	}

	if result.ServerTime == nil {
		t.Error("expected ServerTime to be parsed from Date header")
	}
}

func TestProbeHealthWithCACert(t *testing.T) {
	// Test with a non-existent CA cert file -- should still probe
	result := ProbeHealth(t.Context(), "http://192.0.2.1:1", "/nonexistent/ca.pem")

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
