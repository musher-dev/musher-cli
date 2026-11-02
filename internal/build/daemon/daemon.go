// Package daemon connects to the Docker Engine the way the docker CLI does:
// DOCKER_HOST first, then the active docker context (DOCKER_CONTEXT or the
// config file's currentContext), then the platform default socket.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/connhelper"
	"github.com/moby/moby/client"

	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

const defaultContextName = "default"

// Endpoint is where the Docker Engine was found.
type Endpoint struct {
	// Host is the daemon address, or "" for the platform default socket.
	Host string
	// Source says how Host was chosen: "DOCKER_HOST", "context <name>" or "default".
	Source string
}

// String renders the endpoint for diagnostics.
func (e Endpoint) String() string {
	if e.Host == "" {
		return "default socket"
	}

	return e.Host + " (" + e.Source + ")"
}

// ResolveEndpoint picks the daemon address without dialing it.
func ResolveEndpoint() (Endpoint, error) {
	if host := strings.TrimSpace(env.Get(env.DockerHost)); host != "" {
		return Endpoint{Host: host, Source: env.DockerHost}, nil
	}

	name := strings.TrimSpace(env.Get(env.DockerContext))
	if name == "" {
		name = currentContext()
	}

	if name == "" || name == defaultContextName {
		return Endpoint{Source: defaultContextName}, nil
	}

	host, err := contextHost(config.Dir(), name)
	if err != nil {
		return Endpoint{}, err
	}

	return Endpoint{Host: host, Source: "context " + name}, nil
}

func currentContext() string {
	cfg, err := config.Load(config.Dir())
	if err != nil || cfg == nil {
		return ""
	}

	return cfg.CurrentContext
}

// contextMeta is the slice of a docker context's meta.json this package reads.
type contextMeta struct {
	Endpoints map[string]struct {
		Host string `json:"Host"`
	} `json:"Endpoints"`
}

// contextHost reads the docker endpoint of a named context from the docker CLI
// context store (<config dir>/contexts/meta/<sha256(name)>/meta.json).
func contextHost(configDir, name string) (string, error) {
	sum := sha256.Sum256([]byte(name))
	path := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")

	data, err := os.ReadFile(path) //nolint:gosec // G304: path is derived from the docker config dir and a hashed context name
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", clierrors.Errorf("docker context %q does not exist", name)
		}

		return "", clierrors.Errorf("read docker context %q: %w", name, err)
	}

	var meta contextMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", clierrors.Errorf("parse docker context %q: %w", name, err)
	}

	host := strings.TrimSpace(meta.Endpoints["docker"].Host)
	if host == "" {
		return "", clierrors.Errorf("docker context %q has no docker endpoint", name)
	}

	return host, nil
}

// NewClient returns a Docker Engine API client for the resolved endpoint. It
// does not dial; the first request does.
func NewClient() (*client.Client, Endpoint, error) {
	endpoint, err := ResolveEndpoint()
	if err != nil {
		return nil, Endpoint{}, err
	}

	opts := []client.Opt{client.FromEnv}

	if endpoint.Host != "" {
		helper, helperErr := connhelper.GetConnectionHelper(endpoint.Host)
		if helperErr != nil {
			return nil, endpoint, clierrors.Errorf("docker endpoint %s: %w", endpoint.Host, helperErr)
		}

		if helper != nil {
			opts = append(opts, client.WithHost(helper.Host), client.WithDialContext(helper.Dialer))
		} else {
			opts = append(opts, client.WithHost(endpoint.Host))
		}
	}

	cli, err := client.New(opts...)
	if err != nil {
		return nil, endpoint, clierrors.Errorf("create docker client for %s: %w", endpoint, err)
	}

	return cli, endpoint, nil
}

// Info summarizes the daemon for diagnostics and driver decisions.
type Info struct {
	Endpoint      Endpoint
	ServerVersion string
	APIVersion    string
	OS            string
	Arch          string
	// ContainerdStore reports whether images are kept in the containerd image
	// store, which is what lets BuildKit push multi-manifest images and
	// attestations from the Docker Engine.
	ContainerdStore bool
}

// Probe connects to the daemon and reports its version and image store.
func Probe(ctx context.Context) (Info, error) {
	cli, endpoint, err := NewClient()
	if err != nil {
		return Info{}, err
	}

	defer closeQuietly(cli)

	return probeWith(ctx, cli, endpoint)
}

// infoAPI is the slice of the Docker client Probe needs.
type infoAPI interface {
	ServerVersion(ctx context.Context, opts client.ServerVersionOptions) (client.ServerVersionResult, error)
	Info(ctx context.Context, opts client.InfoOptions) (client.SystemInfoResult, error)
}

func probeWith(ctx context.Context, cli infoAPI, endpoint Endpoint) (Info, error) {
	version, err := cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return Info{Endpoint: endpoint}, clierrors.Errorf("Docker Engine not reachable at %s: %w", endpoint, err)
	}

	info := Info{
		Endpoint:      endpoint,
		ServerVersion: version.Version,
		APIVersion:    version.APIVersion,
		OS:            version.Os,
		Arch:          version.Arch,
	}

	sys, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return info, clierrors.Errorf("read Docker Engine info: %w", err)
	}

	for _, pair := range sys.Info.DriverStatus {
		if len(pair) == 2 && pair[0] == "driver-type" && pair[1] == "io.containerd.snapshotter.v1" {
			info.ContainerdStore = true
		}
	}

	return info, nil
}

func closeQuietly(closer io.Closer) {
	closer.Close()
}
