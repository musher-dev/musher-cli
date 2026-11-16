package buildkit

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/buildkit/client"
	"github.com/tonistiigi/fsutil"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Workers lists the workers of the buildkitd at address, as
// "<id> (<platforms>)", the way `buildctl debug workers` does.
func Workers(ctx context.Context, address string) ([]string, error) {
	bkc, err := client.New(ctx, address)
	if err != nil {
		return nil, clierrors.Errorf("connect to buildkitd at %s: %w", address, err)
	}

	defer closeQuietly(bkc)

	workers, err := bkc.ListWorkers(ctx)
	if err != nil {
		return nil, clierrors.Errorf("list buildkitd workers at %s: %w", address, err)
	}

	names := make([]string, 0, len(workers))

	for _, worker := range workers {
		platforms := make([]string, 0, len(worker.Platforms))
		for _, p := range worker.Platforms {
			platforms = append(platforms, p.OS+"/"+p.Architecture)
		}

		names = append(names, worker.ID+" ("+strings.Join(platforms, ",")+")")
	}

	return names, nil
}

// ScratchBuild builds `FROM scratch` with one copied file on the buildkitd at
// address, exporting nothing: it proves the frontend, the snapshotter and the
// session's file transfer without a registry. dir is an empty scratch
// directory for the build context.
func ScratchBuild(ctx context.Context, address, dir string) error {
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY canary /canary\n"), 0o600); err != nil {
		return clierrors.Errorf("write canary Dockerfile: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "canary"), []byte("musher runner self-test\n"), 0o600); err != nil {
		return clierrors.Errorf("write canary file: %w", err)
	}

	contextFS, err := fsutil.NewFS(dir)
	if err != nil {
		return clierrors.Errorf("open canary context: %w", err)
	}

	bkc, err := client.New(ctx, address)
	if err != nil {
		return clierrors.Errorf("connect to buildkitd at %s: %w", address, err)
	}

	defer closeQuietly(bkc)

	statusCh := make(chan *client.SolveStatus)

	go func() {
		for range statusCh { //nolint:revive // drain: the canary keeps no progress
		}
	}()

	_, err = bkc.Solve(ctx, nil, client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: map[string]string{"filename": "Dockerfile", "platform": "linux/amd64"},
		LocalMounts:   map[string]fsutil.FS{"context": contextFS, "dockerfile": contextFS},
	}, statusCh)
	if err != nil {
		return clierrors.Errorf("FROM scratch build: %w", err)
	}

	return nil
}
