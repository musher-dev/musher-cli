package client

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewInstrumentedHTTPClientNoCA(t *testing.T) {
	t.Parallel()

	c, err := NewInstrumentedHTTPClient("")
	if err != nil {
		t.Fatalf("NewInstrumentedHTTPClient error = %v", err)
	}

	if c == nil {
		t.Fatal("client is nil")
	}
}

func TestNewInstrumentedHTTPClientWhitespaceCA(t *testing.T) {
	t.Parallel()

	c, err := NewInstrumentedHTTPClient("   ")
	if err != nil {
		t.Fatalf("NewInstrumentedHTTPClient error = %v", err)
	}

	if c == nil {
		t.Fatal("client is nil")
	}
}

func TestNewInstrumentedHTTPClientNonExistentCA(t *testing.T) {
	t.Parallel()

	_, err := NewInstrumentedHTTPClient("/nonexistent/ca.pem")
	if err == nil {
		t.Fatal("expected error for non-existent CA cert file")
	}
}

func TestNewInstrumentedHTTPClientInvalidCA(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "bad-ca.pem")

	if err := os.WriteFile(caPath, []byte("not a valid PEM"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := NewInstrumentedHTTPClient(caPath)
	if err == nil {
		t.Fatal("expected error for invalid CA cert file")
	}
}

func TestNewStreamingHTTPClient(t *testing.T) {
	t.Parallel()

	streaming, err := NewStreamingHTTPClient("")
	if err != nil {
		t.Fatalf("NewStreamingHTTPClient error = %v", err)
	}

	// A non-zero Timeout would cut every SSE stream at that mark, which reads
	// like a server bug rather than a client one.
	if streaming.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0 for a streaming client", streaming.Timeout)
	}

	transport, ok := streaming.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", streaming.Transport)
	}

	if transport.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 30s", transport.ResponseHeaderTimeout)
	}

	if !transport.DisableCompression {
		t.Error("compression must be disabled so events are delivered incrementally")
	}

	// The non-streaming client must be unaffected.
	regular, err := NewInstrumentedHTTPClient("")
	if err != nil {
		t.Fatalf("NewInstrumentedHTTPClient error = %v", err)
	}

	if regular.Timeout != DefaultTimeout {
		t.Errorf("regular client Timeout = %v, want %v", regular.Timeout, DefaultTimeout)
	}
}

func TestNewStreamingHTTPClientInvalidCA(t *testing.T) {
	t.Parallel()

	if _, err := NewStreamingHTTPClient("/nonexistent/ca.pem"); err == nil {
		t.Fatal("expected error for non-existent CA cert file")
	}
}

func TestNewInstrumentedHTTPClientWithTimeout(t *testing.T) {
	t.Parallel()

	c, err := NewInstrumentedHTTPClientWithTimeout("", 5*time.Second)
	if err != nil {
		t.Fatalf("NewInstrumentedHTTPClientWithTimeout error = %v", err)
	}

	if c == nil {
		t.Fatal("client is nil")
	}

	if c.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want %v", c.Timeout, 5*time.Second)
	}
}
