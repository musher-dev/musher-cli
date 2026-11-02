package build

import (
	"context"
	"strings"
	"time"

	"github.com/musher-dev/musher-cli/internal/build/buildkit"
	"github.com/musher-dev/musher-cli/internal/build/daemon"
	"github.com/musher-dev/musher-cli/internal/build/registry"
)

// probeTimeout bounds each diagnostic so an unreachable daemon cannot stall
// `musher doctor`.
const probeTimeout = 5 * time.Second

// CheckStatus is the outcome of a build diagnostic.
type CheckStatus int

const (
	// CheckPass means the build engine is ready in this respect.
	CheckPass CheckStatus = iota
	// CheckWarn means builds may fail or degrade; nothing else is affected.
	CheckWarn
	// CheckFail means builds cannot work.
	CheckFail
)

// CheckResult is an output-agnostic diagnostic result; internal/doctor renders it.
type CheckResult struct {
	Status  CheckStatus
	Message string
	Detail  string
}

// Probes are the environment lookups the checks depend on, replaceable in tests.
type Probes struct {
	Daemon         func(ctx context.Context) (daemon.Info, error)
	Standalone     func(ctx context.Context, address string) (string, error)
	HasCredentials func(host string) bool
}

// DefaultProbes reach the real Docker Engine, buildkitd and credential store.
func DefaultProbes() Probes {
	return Probes{
		Daemon:         daemon.Probe,
		Standalone:     buildkit.ProbeStandalone,
		HasCredentials: registry.HasLocalCredentials,
	}
}

// CheckBuilder reports whether a builder is reachable: the configured
// standalone buildkitd, or the Docker Engine and its version.
func CheckBuilder(ctx context.Context, buildkitAddress string, probes Probes) CheckResult {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	if addr, source := buildkit.ResolveAddress(buildkitAddress); addr != "" {
		version, err := probes.Standalone(ctx, addr)
		if err != nil {
			return CheckResult{
				Status:  CheckWarn,
				Message: "buildkitd not reachable at " + addr + " (from " + source + ")",
				Detail:  err.Error(),
			}
		}

		return CheckResult{Status: CheckPass, Message: "buildkitd " + version + " at " + addr}
	}

	info, err := probes.Daemon(ctx)
	if err != nil {
		return CheckResult{
			Status:  CheckWarn,
			Message: "Docker Engine not reachable; 'musher build' needs Docker or a buildkitd (build.buildkit_host)",
			Detail:  err.Error(),
		}
	}

	return CheckResult{
		Status:  CheckPass,
		Message: "Docker Engine " + info.ServerVersion + " (API " + info.APIVersion + ") at " + info.Endpoint.String(),
	}
}

// CheckImageStore reports whether the Docker Engine keeps images in the
// containerd image store. The classic store still builds and pushes, but its
// exporter reports the image ID rather than the manifest digest, so the digest
// is read back from the registry, and it cannot hold attestations.
func CheckImageStore(ctx context.Context, buildkitAddress string, probes Probes) CheckResult {
	if addr, _ := buildkit.ResolveAddress(buildkitAddress); addr != "" {
		return CheckResult{Status: CheckPass, Message: "not applicable (standalone buildkitd)"}
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	info, err := probes.Daemon(ctx)
	if err != nil {
		return CheckResult{Status: CheckWarn, Message: "unknown (Docker Engine not reachable)"}
	}

	if info.ContainerdStore {
		return CheckResult{Status: CheckPass, Message: "containerd image store"}
	}

	return CheckResult{
		Status:  CheckWarn,
		Message: "classic image store",
		Detail:  "Builds work, but enable the containerd image store in Docker for multi-platform images and attestations",
	}
}

// CheckRegistryCredentials reports whether the docker credential store holds
// credentials for the host of repository (config build.repository).
func CheckRegistryCredentials(repository string, probes Probes) CheckResult {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return CheckResult{Status: CheckPass, Message: "skipped (build.repository not set)"}
	}

	host, err := registry.HostOf(repository)
	if err != nil {
		return CheckResult{Status: CheckFail, Message: "build.repository is invalid", Detail: err.Error()}
	}

	if isLocalHost(host) {
		return CheckResult{Status: CheckPass, Message: host + " (local registry)"}
	}

	if probes.HasCredentials(host) {
		return CheckResult{Status: CheckPass, Message: "docker credentials found for " + host}
	}

	return CheckResult{
		Status:  CheckWarn,
		Message: "no docker credentials for " + host,
		Detail:  "Run 'docker login " + host + "' before pushing",
	}
}

func isLocalHost(host string) bool {
	hostname := host
	if idx := strings.LastIndex(host, ":"); idx >= 0 {
		hostname = host[:idx]
	}

	return hostname == "localhost" || strings.HasPrefix(hostname, "127.")
}
