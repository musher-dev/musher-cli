package client

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"strings"
	"time"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// NewInstrumentedHTTPClient creates an HTTP client with optional custom CA bundle support.
func NewInstrumentedHTTPClient(caCertFile string) (*http.Client, error) {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, repoerrors.Errorf("default transport type %T is not *http.Transport", http.DefaultTransport)
	}

	transport := baseTransport.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	customCAPath := strings.TrimSpace(caCertFile)
	if customCAPath != "" {
		pemData, err := safeio.ReadFile(customCAPath)
		if err != nil {
			return nil, repoerrors.Errorf("read CA cert file %q: %w", customCAPath, err)
		}

		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}

		if ok := pool.AppendCertsFromPEM(pemData); !ok {
			return nil, repoerrors.Errorf("parse CA cert file %q: no certificates found", customCAPath)
		}

		transport.TLSClientConfig.RootCAs = pool
	}

	return &http.Client{
		Timeout:   DefaultTimeout,
		Transport: transport,
	}, nil
}

// streamingHeaderTimeout bounds how long a streaming request may wait for
// response headers. The stream itself is unbounded; only the handshake is not.
const streamingHeaderTimeout = 30 * time.Second

// NewStreamingHTTPClient returns a client with NO overall timeout, for SSE.
//
// http.Client.Timeout covers the entire exchange including the response body,
// so the shared DefaultTimeout of 60s would sever every server-sent-event
// stream at exactly one minute. That failure looks like a server bug — logs
// stop mid-deployment with no error from either side — which is why streaming
// gets its own client instead of a larger shared timeout.
//
// ResponseHeaderTimeout keeps a hung connect from hanging forever: the server
// must produce headers within 30s, after which the stream may run as long as
// it likes and is bounded only by the caller's context.
func NewStreamingHTTPClient(caCertFile string) (*http.Client, error) {
	streaming, err := NewInstrumentedHTTPClient(caCertFile)
	if err != nil {
		return nil, err
	}

	streaming.Timeout = 0

	if transport, ok := streaming.Transport.(*http.Transport); ok {
		transport.ResponseHeaderTimeout = streamingHeaderTimeout
		// Compression would buffer the stream, defeating incremental delivery.
		transport.DisableCompression = true
	}

	return streaming, nil
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
