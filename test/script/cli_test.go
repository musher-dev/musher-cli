package script_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/rogpeppe/go-internal/testscript"
)

var (
	buildOnce    sync.Once
	buildPath    string
	errBuildFail error
)

func TestCLI(t *testing.T) {
	t.Parallel()

	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		RequireExplicitExec: true,
		Setup:               setupCLI,
	})
}

func TestDocs(t *testing.T) {
	t.Parallel()

	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/docs",
		RequireExplicitExec: true,
		Setup:               setupCLI,
	})
}

func setupCLI(env *testscript.Env) error {
	homeDir := filepath.Join(env.WorkDir, "home")
	configDir := filepath.Join(env.WorkDir, "config")
	dataDir := filepath.Join(env.WorkDir, "data")
	cacheDir := filepath.Join(env.WorkDir, "cache")
	stateDir := filepath.Join(env.WorkDir, "state")

	for _, dir := range []string{homeDir, configDir, dataDir, cacheDir, stateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return repoerrors.Errorf("create test dir %s: %w", dir, err)
		}
	}

	binPath, err := buildCLI()
	if err != nil {
		return err
	}

	env.Setenv("HOME", homeDir)
	env.Setenv("MUSHER_CONFIG_HOME", configDir)
	env.Setenv("MUSHER_DATA_HOME", dataDir)
	env.Setenv("MUSHER_CACHE_HOME", cacheDir)
	env.Setenv("MUSHER_STATE_HOME", stateDir)
	env.Setenv("MUSHER_BIN", binPath)
	env.Setenv("MUSHER_API_URL", "http://127.0.0.1:1")

	return nil
}

func buildCLI() (string, error) {
	buildOnce.Do(func() {
		exeSuffix := ""
		if runtime.GOOS == "windows" {
			exeSuffix = ".exe"
		}

		repoRoot, err := repoRoot()
		if err != nil {
			errBuildFail = err
			return
		}

		binDir, err := os.MkdirTemp("", "musher-script-bin-")
		if err != nil {
			errBuildFail = repoerrors.Errorf("create script bin dir: %w", err)
			return
		}

		buildPath = filepath.Join(binDir, "musher"+exeSuffix)

		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", buildPath, "./cmd/musher")
		cmd.Dir = repoRoot

		cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(os.TempDir(), "musher-script-gocache"))

		output, err := cmd.CombinedOutput()
		if err != nil {
			errBuildFail = repoerrors.Errorf("build musher test binary: %w\n%s", err, output)
		}
	})

	if errBuildFail != nil {
		return "", errBuildFail
	}

	return buildPath, nil
}

func repoRoot() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", repoerrors.Errorf("resolve test file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..")), nil
}
