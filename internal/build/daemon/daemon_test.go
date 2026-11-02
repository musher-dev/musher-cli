package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"
)

func isolate(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")

	return dir
}

func writeContext(t *testing.T, configDir, name, meta string) {
	t.Helper()

	sum := sha256.Sum256([]byte(name))
	dir := filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(sum[:]))

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveEndpointPrecedence(t *testing.T) {
	dir := isolate(t)

	got, err := ResolveEndpoint()
	if err != nil || got.Host != "" || got.Source != "default" || got.String() != "default socket" {
		t.Fatalf("default = %+v, %v", got, err)
	}

	writeContext(t, dir, "desktop-linux", `{"Name":"desktop-linux","Endpoints":{"docker":{"Host":"unix:///Users/me/.docker/run/docker.sock"}}}`)

	if err = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"currentContext":"desktop-linux"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err = ResolveEndpoint()
	if err != nil || got.Host != "unix:///Users/me/.docker/run/docker.sock" || got.Source != "context desktop-linux" {
		t.Fatalf("config context = %+v, %v", got, err)
	}

	if !strings.Contains(got.String(), "context desktop-linux") {
		t.Errorf("String() = %q", got.String())
	}

	writeContext(t, dir, "remote", `{"Endpoints":{"docker":{"Host":"tcp://10.0.0.1:2376"}}}`)
	t.Setenv("DOCKER_CONTEXT", "remote")

	if got, err = ResolveEndpoint(); err != nil || got.Host != "tcp://10.0.0.1:2376" {
		t.Fatalf("DOCKER_CONTEXT = %+v, %v", got, err)
	}

	t.Setenv("DOCKER_CONTEXT", "default")

	if got, err = ResolveEndpoint(); err != nil || got.Host != "" {
		t.Fatalf("default context = %+v, %v", got, err)
	}

	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:2375")

	if got, err = ResolveEndpoint(); err != nil || got.Host != "tcp://127.0.0.1:2375" || got.Source != "DOCKER_HOST" {
		t.Fatalf("DOCKER_HOST = %+v, %v", got, err)
	}
}

func TestContextHostErrors(t *testing.T) {
	dir := isolate(t)

	if _, err := contextHost(dir, "missing"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing = %v", err)
	}

	writeContext(t, dir, "garbage", "{not json")

	if _, err := contextHost(dir, "garbage"); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("garbage = %v", err)
	}

	writeContext(t, dir, "k8s", `{"Endpoints":{"kubernetes":{}}}`)

	if _, err := contextHost(dir, "k8s"); err == nil || !strings.Contains(err.Error(), "no docker endpoint") {
		t.Errorf("no endpoint = %v", err)
	}

	t.Setenv("DOCKER_CONTEXT", "missing")

	if _, err := ResolveEndpoint(); err == nil {
		t.Error("an unknown DOCKER_CONTEXT should fail")
	}

	if _, _, err := NewClient(); err == nil {
		t.Error("NewClient should surface the context error")
	}
}

func TestNewClientHosts(t *testing.T) {
	isolate(t)

	for _, host := range []string{"", "tcp://127.0.0.1:1", "ssh://user@example.invalid"} {
		t.Setenv("DOCKER_HOST", host)

		cli, endpoint, err := NewClient()
		if err != nil {
			t.Fatalf("NewClient(%q): %v", host, err)
		}

		if endpoint.Host != host {
			t.Errorf("endpoint = %+v, want host %q", endpoint, host)
		}

		closeQuietly(cli)
	}

	t.Setenv("DOCKER_HOST", "tcp://[::1")

	if _, _, err := NewClient(); err == nil {
		t.Error("an unparsable host should fail")
	}
}

type fakeInfo struct {
	version    client.ServerVersionResult
	versionErr error
	info       client.SystemInfoResult
	infoErr    error
}

func (f *fakeInfo) ServerVersion(context.Context, client.ServerVersionOptions) (client.ServerVersionResult, error) {
	return f.version, f.versionErr
}

func (f *fakeInfo) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	return f.info, f.infoErr
}

func TestProbeWith(t *testing.T) {
	t.Parallel()

	endpoint := Endpoint{Source: "default"}
	version := client.ServerVersionResult{Version: "29.0.0", APIVersion: "1.52", Os: "linux", Arch: "amd64"}

	got, err := probeWith(t.Context(), &fakeInfo{
		version: version,
		info:    client.SystemInfoResult{Info: system.Info{DriverStatus: [][2]string{{"driver-type", "io.containerd.snapshotter.v1"}}}},
	}, endpoint)
	if err != nil || !got.ContainerdStore || got.ServerVersion != "29.0.0" || got.APIVersion != "1.52" || got.OS != "linux" || got.Arch != "amd64" {
		t.Fatalf("containerd = %+v, %v", got, err)
	}

	got, err = probeWith(t.Context(), &fakeInfo{version: version}, endpoint)
	if err != nil || got.ContainerdStore {
		t.Fatalf("classic = %+v, %v", got, err)
	}

	if _, err := probeWith(t.Context(), &fakeInfo{versionErr: errors.New("refused")}, endpoint); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("version error = %v", err)
	}

	if _, err := probeWith(t.Context(), &fakeInfo{version: version, infoErr: errors.New("boom")}, endpoint); err == nil {
		t.Error("info error should fail")
	}
}

func TestProbeUnreachable(t *testing.T) {
	isolate(t)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "none.sock"))

	if _, err := Probe(t.Context()); err == nil {
		t.Error("Probe of a missing socket should fail")
	}

	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "missing")

	if _, err := Probe(t.Context()); err == nil {
		t.Error("Probe with an unknown context should fail")
	}
}
