package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/safeio"
)

// NewInstrumentedHTTPClient creates an HTTP client with optional custom CA bundle support.
func NewInstrumentedHTTPClient(caCertFile string) (*http.Client, error) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default transport type %T is not *http.Transport", http.DefaultTransport)
	}

	transport := baseTransport.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	customCAPath := strings.TrimSpace(caCertFile)
	if customCAPath != "" {
		pemData, err := safeio.ReadFile(customCAPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert file %q: %w", customCAPath, err)
		}

		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}

		if ok := pool.AppendCertsFromPEM(pemData); !ok {
			return nil, fmt.Errorf("parse CA cert file %q: no certificates found", customCAPath)
		}

		transport.TLSClientConfig.RootCAs = pool
	}

	return &http.Client{
		Timeout:   DefaultTimeout,
		Transport: transport,
	}, nil
}

// NewInstrumentedHTTPClientWithTimeout creates an HTTP client with custom timeout and optional CA bundle.
func NewInstrumentedHTTPClientWithTimeout(caCertFile string, timeout time.Duration) (*http.Client, error) {
	c, err := NewInstrumentedHTTPClient(caCertFile)
	if err != nil {
		return nil, err
	}

	c.Timeout = timeout

	return c, nil
}
