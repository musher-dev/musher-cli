package client

import (
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
