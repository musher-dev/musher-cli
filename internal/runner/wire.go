package runner

import (
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
)

// The HTTP client every non-API request uses: forge downloads, presigned
// storage uploads and downloads. It carries no credential of its own.
func plainHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Minute}
}

// NewFetcher wires a Fetcher from the guest's environment.
func NewFetcher(environ *Environment, log *slog.Logger) (*Fetcher, error) {
	if err := environ.RequireAPI(); err != nil {
		return nil, err
	}

	client, err := NewClient(environ.APIURL, environ.Token, nil, DefaultRetry)
	if err != nil {
		return nil, err
	}

	expiry, _ := TokenExpiry(environ.Token)

	return &Fetcher{
		Client:      client,
		HTTP:        plainHTTPClient(),
		ScratchDir:  environ.ScratchDir,
		TokenExpiry: expiry,
		Retry:       RetryPolicy{Attempts: 3, Base: 2 * time.Second, Max: 8 * time.Second},
		Log:         log,
	}, nil
}

// NewBuilder wires a Builder from the guest's environment, building on the
// buildkitd at buildkitHost. progress, when set, receives BuildKit's plain
// rendering (the guest's console).
func NewBuilder(environ *Environment, buildkitHost string, progress io.Writer, log *slog.Logger) (*Builder, error) {
	if err := environ.RequireAPI(); err != nil {
		return nil, err
	}

	client, err := NewClient(environ.APIURL, environ.Token, nil, DefaultRetry)
	if err != nil {
		return nil, err
	}

	expiry, _ := TokenExpiry(environ.Token)

	return &Builder{
		Client:      client,
		HTTP:        plainHTTPClient(),
		Engine:      &BuildKitEngine{Address: buildkitHost, Progress: progress},
		ScratchDir:  environ.ScratchDir,
		Token:       environ.Token,
		TokenExpiry: expiry,
		Platform:    "linux/amd64",
		LogInterval: 2 * time.Second,
		Log:         log,
	}, nil
}

// NewSelfTest wires a SelfTest from the guest's environment.
func NewSelfTest(environ *Environment, buildkitHost, registryHost string) *SelfTest {
	return &SelfTest{
		Version:      buildinfo.Version,
		BuildKitHost: buildkitHost,
		APIURL:       environ.APIURL,
		RegistryHost: registryHost,
		ScratchDir:   environ.ScratchDir,
		DenyMetadata: DefaultDenyMetadata,
		DenyPrivate:  DefaultDenyPrivate,
		DenyCGNAT:    DefaultDenyCGNAT,
	}
}
