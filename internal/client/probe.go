package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/safeio"
)

const probeTimeout = 3 * time.Second

// ProbeResult holds the outcome of a lightweight API health probe.
type ProbeResult struct {
	// Host is the hostname that was probed (e.g. "api.musher.dev").
	Host string

	// Reachable is true if any HTTP response was received (even 4xx/5xx).
	Reachable bool

	// Latency is the round-trip time when Reachable is true.
	Latency time.Duration

	// StatusCode is the received HTTP status code when reachable.
	StatusCode int

	// ServerTime is parsed from the HTTP Date header when present.
	ServerTime *time.Time

	// Error is a user-friendly error summary when Reachable is false.
	Error string
}

// ProbeHealth performs a lightweight connectivity check against baseURL.
// Any HTTP response (including 4xx/5xx) counts as reachable — only
// network-level failures (DNS, TCP, TLS) are treated as unreachable.
// The probe uses its own http.Client with no auth and a short timeout.
// An optional caCertFile can be provided to honor custom CA bundles
// (e.g. from network.ca_cert_file config), ensuring probe TLS behavior
// matches the main API client.
func ProbeHealth(ctx context.Context, baseURL string, caCertFile ...string) *ProbeResult {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return &ProbeResult{
			Host:  baseURL,
			Error: "invalid URL",
		}
	}

	host := parsed.Hostname()
	if host == "" {
		return &ProbeResult{
			Host:  baseURL,
			Error: "invalid URL",
		}
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &ProbeResult{
			Host:  host,
			Error: "unable to create HTTP transport",
		}
	}

	cloned := transport.Clone()

	if len(caCertFile) > 0 {
		caPath := strings.TrimSpace(caCertFile[0])
		if caPath != "" {
			tlsCfg, tlsErr := buildProbeTLSConfig(caPath)
			if tlsErr != nil {
				return &ProbeResult{
					Host:  host,
					Error: fmt.Sprintf("custom CA bundle error: %v", tlsErr),
				}
			}

			cloned.TLSClientConfig = tlsCfg
		}
	}

	httpClient := &http.Client{
		Timeout:   probeTimeout,
		Transport: cloned,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, http.NoBody)
	if err != nil {
		return &ProbeResult{
			Host:  host,
			Error: summarizeNetworkError(err),
		}
	}

	start := time.Now()

	resp, err := httpClient.Do(req)
	if err != nil {
		return &ProbeResult{
			Host:  host,
			Error: summarizeNetworkError(err),
		}
	}
	defer resp.Body.Close()

	return &ProbeResult{
		Host:       host,
		Reachable:  true,
		Latency:    time.Since(start),
		StatusCode: resp.StatusCode,
		ServerTime: parseHTTPDate(resp.Header.Get("Date")),
	}
}

func parseHTTPDate(raw string) *time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}

	layouts := []string{time.RFC1123, time.RFC1123Z, time.RFC850, time.ANSIC}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			utc := parsed.UTC()
			return &utc
		}
	}

	return nil
}

// summarizeNetworkError translates Go network errors into concise,
// user-friendly messages.
func summarizeNetworkError(err error) string {
	if err == nil {
		return ""
	}

	// Check typed errors first (before calling .Error() which may panic
	// on incomplete structs like x509.HostnameError with nil Certificate).
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "DNS resolution failed"
	}

	var certErr *x509.UnknownAuthorityError
	if errors.As(err, &certErr) {
		return "TLS certificate error"
	}

	var certHostErr *x509.HostnameError
	if errors.As(err, &certHostErr) {
		return "TLS certificate error"
	}

	// Fall back to string matching for common patterns.
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "connection refused"):
		return "connection refused"
	case strings.Contains(lower, "certificate"):
		return "TLS certificate error"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "connection timed out"
	case strings.Contains(lower, "no such host"):
		return "DNS resolution failed"
	}

	// Truncate long messages.
	if len(msg) > 120 {
		return msg[:120]
	}

	return msg
}

// buildProbeTLSConfig creates a TLS config that appends custom CA certs
// to the system pool — mirroring what NewInstrumentedHTTPClient does.
func buildProbeTLSConfig(caPath string) (*tls.Config, error) {
	pemData, err := safeio.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read probe CA cert: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	if ok := pool.AppendCertsFromPEM(pemData); !ok {
		return nil, fmt.Errorf("parse probe CA cert %q: no certificates found", caPath)
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
	}, nil
}
