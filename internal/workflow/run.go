package workflow

import (
	"context"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
	"github.com/musher-dev/musher-cli/internal/bundle/session"
	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
	"github.com/musher-dev/musher-cli/internal/harness/runtime"
	"github.com/musher-dev/musher-cli/internal/transcript"
)

// SessionDeps holds injected dependencies for running a bundle session.
type SessionDeps struct {
	Progress        ProgressFactory
	PullDeps        *PullDeps
	Harness         *harness.Provider
	ProjectDir      string
	NoWatch         bool
	IsTTY           bool
	ScrollbackLines int
	TranscriptDir   string
}

// SessionResult holds the outcome of a single session run.
type SessionResult struct {
	ExitCode int
}

// RunSingleSession pulls a bundle, materializes assets, and executes a harness.
// It always cleans up injected assets before returning.
func RunSingleSession(ctx context.Context, deps *SessionDeps, ref string, force bool) (*SessionResult, error) {
	namespace, slug, bundleVersion, err := ParseBundleRef(ref)
	if err != nil {
		return nil, err
	}

	// Pull bundle to cache.
	result, err := PullToCache(ctx, deps.PullDeps, namespace, slug, bundleVersion, force)
	if err != nil {
		return nil, err
	}

	versionRef := result.Namespace + "/" + result.Slug + ":" + result.Version

	// Load manifest from cache.
	store, manifest, err := LoadManifestFromCache(result)
	if err != nil {
		return nil, err
	}

	// Materialize assets.
	prov := deps.Harness
	spin := deps.Progress.NewProgress("Preparing " + versionRef + " for " + prov.Spec.DisplayName)
	spin.Start()

	loadSession, err := session.PrepareLoadSession(store, prov.Spec, prov.AgentTransform, prov.AgentFileExt, manifest, deps.ProjectDir)
	if err != nil {
		spin.StopWithFailure("Preparation failed")

		return nil, clierrors.Wrap(clierrors.ExitExecution,
			"Failed to prepare bundle for "+prov.Spec.DisplayName, err)
	}

	defer loadSession.Cleanup()

	spin.StopWithSuccess("Ready")

	// Build harness command.
	args := BuildHarnessArgs(loadSession, prov.Spec)

	// Set up transcript recording.
	var transcriptWriter runtime.TranscriptWriter

	if deps.TranscriptDir != "" {
		tStore := transcript.NewStore(deps.TranscriptDir)
		if sess, createErr := tStore.Create(versionRef); createErr == nil {
			if writer, openErr := tStore.OpenWriter(sess.ID); openErr == nil {
				transcriptWriter = writer

				defer func() {
					writer.Close()        //nolint:errcheck // best-effort close
					tStore.Close(sess.ID) //nolint:errcheck // best-effort close
				}()
			}
		}
	}

	// Select and launch executor.
	cfg := &runtime.Config{
		Binary:          prov.Spec.Binary,
		Args:            args,
		WorkDir:         deps.ProjectDir,
		ExtraEnv:        loadSession.Env,
		BundleRef:       versionRef,
		HarnessName:     prov.Spec.DisplayName,
		Transcript:      transcriptWriter,
		ScrollbackLines: deps.ScrollbackLines,
	}

	executor := runtime.Select(deps.IsTTY, deps.NoWatch)

	exitCode, err := executor.Run(ctx, cfg)
	if err != nil {
		return &SessionResult{ExitCode: exitCode}, clierrors.Errorf("harness execution: %w", err)
	}

	return &SessionResult{ExitCode: exitCode}, nil
}

// ParseBundleRef parses "namespace/slug" or "namespace/slug:version".
// If no version is provided, version is returned as "".
func ParseBundleRef(raw string) (namespace, slug, version string, err error) {
	ref, parseErr := bundledef.ParseRefOptionalVersion(raw)
	if parseErr != nil {
		return "", "", "", &clierrors.CLIError{
			Message: parseErr.Error(),
			Hint:    "Use <namespace/slug> or <namespace/slug:version>",
			Code:    clierrors.ExitUsage,
		}
	}

	return ref.Namespace, ref.Slug, ref.Version, nil
}

// LoadManifestFromCache creates a cache store and loads the bundle manifest.
func LoadManifestFromCache(result *pull.Result) (*cache.Store, *cache.BundleManifest, error) {
	store, err := cache.NewStore(result.CacheRoot)
	if err != nil {
		return nil, nil, clierrors.Wrap(clierrors.ExitConfig, "Failed to open cache store", err)
	}

	manifest, err := store.LoadManifest(result.HostID, result.Namespace, result.Slug, result.Version)
	if err != nil {
		return nil, nil, clierrors.CacheCorrupt(
			result.Namespace+"/"+result.Slug+":"+result.Version, err)
	}

	return store, manifest, nil
}

// BuildHarnessArgs constructs the CLI arguments for the harness binary.
func BuildHarnessArgs(s *session.LoadSession, spec *harness.Spec) []string {
	var args []string

	args = append(args, s.HarnessDirArgs(spec.BundleDir.Flag)...)
	args = append(args, s.ToolConfigArgs()...)
	args = append(args, s.AgentArgs()...)

	return args
}
