package harness

import (
	"context"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/musher-dev/musher-cli/internal/paths"
)

// CheckStatus represents the outcome of a single health check.
type CheckStatus int

const (
	// CheckPass indicates the check succeeded.
	CheckPass CheckStatus = iota
	// CheckWarn indicates a non-critical issue.
	CheckWarn
	// CheckFail indicates a critical failure.
	CheckFail
)

// healthCheckTimeout is the maximum time to wait for a version command.
const healthCheckTimeout = 5 * time.Second

// HealthCheck is the result of a single diagnostic check.
type HealthCheck struct {
	Name    string
	Status  CheckStatus
	Message string
}

// HealthReport is the aggregate health of a single provider.
type HealthReport struct {
	ProviderName string
	DisplayName  string
	Installed    bool
	Version      string
	Checks       []HealthCheck
}

// CheckHealth runs all health checks for a single provider.
func CheckHealth(ctx context.Context, prov *Provider) *HealthReport {
	spec := prov.Spec
	report := &HealthReport{
		ProviderName: spec.Name,
		DisplayName:  spec.DisplayName,
	}

	// 1. Binary check.
	binaryPath, err := exec.LookPath(spec.Binary)
	if err != nil {
		report.Checks = append(report.Checks, HealthCheck{
			Name:    "binary",
			Status:  CheckFail,
			Message: spec.Binary + " not found on PATH",
		})

		// No point checking version if binary isn't found.
		report.Installed = false

		return report
	}

	report.Installed = true
	report.Checks = append(report.Checks, HealthCheck{
		Name:    "binary",
		Status:  CheckPass,
		Message: binaryPath,
	})

	// 2. Version check.
	if len(spec.Status.VersionArgs) > 0 {
		version := detectVersion(ctx, spec.Binary, spec.Status.VersionArgs)
		if version != "" {
			report.Version = version
			report.Checks = append(report.Checks, HealthCheck{
				Name:    "version",
				Status:  CheckPass,
				Message: version,
			})
		} else {
			report.Checks = append(report.Checks, HealthCheck{
				Name:    "version",
				Status:  CheckWarn,
				Message: "could not determine version",
			})
		}
	}

	// 3. Config directory check.
	if spec.Status.ConfigDir != "" {
		report.Checks = append(report.Checks, checkPath("config", spec.Status.ConfigDir, "configuration directory"))
	}

	// 4. Auth check.
	if spec.Status.AuthCheck.Path != "" {
		desc := spec.Status.AuthCheck.Description
		if desc == "" {
			desc = "credentials"
		}

		report.Checks = append(report.Checks, checkPath("auth", spec.Status.AuthCheck.Path, desc))
	}

	return report
}

// CheckAllHealth runs health checks for all providers concurrently.  The
// per-provider checks each respect the supplied context, so canceling it
// (e.g. via a bounded timeout) cleanly stops in-flight version subprocesses.
func CheckAllHealth(ctx context.Context, reg *Registry) []*HealthReport {
	providers := reg.List()
	results := make([]*HealthReport, len(providers))

	group, gctx := errgroup.WithContext(ctx)

	for idx, prov := range providers {
		group.Go(func() error {
			results[idx] = CheckHealth(gctx, prov)
			return nil
		})
	}

	// CheckHealth never returns an error, so group.Wait is purely a join.
	_ = group.Wait() //nolint:errcheck // see above

	sort.Slice(results, func(i, j int) bool {
		return results[i].ProviderName < results[j].ProviderName
	})

	return results
}

// detectVersion runs the binary with version args and extracts the first line.
func detectVersion(ctx context.Context, binary string, args []string) string {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // binary from trusted harness spec

	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	line := strings.TrimSpace(string(out))
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}

	return line
}

// checkPath verifies a tilde-expanded path exists.
func checkPath(name, path, description string) HealthCheck {
	expanded, err := paths.ExpandTilde(path)
	if err != nil {
		return HealthCheck{
			Name:    name,
			Status:  CheckWarn,
			Message: "cannot resolve path: " + err.Error(),
		}
	}

	if _, err := os.Stat(expanded); err != nil {
		return HealthCheck{
			Name:    name,
			Status:  CheckWarn,
			Message: description + " not found",
		}
	}

	return HealthCheck{
		Name:    name,
		Status:  CheckPass,
		Message: description + " found",
	}
}
