package client

import (
	"crypto/x509"
	"errors"
	"net"
	"testing"
	"time"
)

func TestParseHTTPDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		wantNi bool // want nil
		wantTS string
	}{
		{"empty string", "", true, ""},
		{"whitespace only", "   ", true, ""},
		{"RFC1123", "Mon, 02 Jan 2006 15:04:05 GMT", false, "2006-01-02T15:04:05Z"},
		{"RFC1123Z", "Mon, 02 Jan 2006 15:04:05 +0000", false, "2006-01-02T15:04:05Z"},
		{"RFC850", "Monday, 02-Jan-06 15:04:05 GMT", false, "2006-01-02T15:04:05Z"},
		{"ANSIC", "Mon Jan  2 15:04:05 2006", false, "2006-01-02T15:04:05Z"},
		{"invalid format", "not-a-date", true, ""},
		{"with surrounding spaces", "  Mon, 02 Jan 2006 15:04:05 GMT  ", false, "2006-01-02T15:04:05Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := parseHTTPDate(tt.input)
			if tt.wantNi {
				if result != nil {
					t.Errorf("parseHTTPDate(%q) = %v, want nil", tt.input, result)
				}

				return
			}

			if result == nil {
				t.Fatalf("parseHTTPDate(%q) = nil, want non-nil", tt.input)
			}

			got := result.Format(time.RFC3339)
			if got != tt.wantTS {
				t.Errorf("parseHTTPDate(%q) = %q, want %q", tt.input, got, tt.wantTS)
			}
		})
	}
}

func TestSummarizeNetworkError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{
			"DNS error",
			&net.DNSError{Err: "no such host", Name: "bad.example.com"},
			"DNS resolution failed",
		},
		{
			"unknown authority",
			&x509.UnknownAuthorityError{},
			"TLS certificate error",
		},
		{
			"hostname error",
			&x509.HostnameError{Host: "example.com"},
			"TLS certificate error",
		},
		{
			"connection refused",
			errors.New("dial tcp 127.0.0.1:443: connection refused"),
			"connection refused",
		},
		{
			"certificate in message",
			errors.New("tls: certificate is not trusted"),
			"TLS certificate error",
		},
		{
			"timeout",
			errors.New("i/o timeout"),
			"connection timed out",
		},
		{
			"deadline exceeded",
			errors.New("context deadline exceeded"),
			"connection timed out",
		},
		{
			"no such host in message",
			errors.New("dial tcp: lookup bad.test: no such host"),
			"DNS resolution failed",
		},
		{
			"generic short error",
			errors.New("something broke"),
			"something broke",
		},
		{
			"long error truncated",
			errors.New(string(make([]byte, 200))),
			string(make([]byte, 120)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := summarizeNetworkError(tt.err)
			if got != tt.want {
				t.Errorf("summarizeNetworkError() = %q, want %q", got, tt.want)
			}
		})
	}
}
