package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

func newPullCmd() *cobra.Command {
	var (
		outputDir string
		force     bool
	)

	cmd := &cobra.Command{
		Use:   "pull <namespace/slug[:version]>",
		Short: "Download a bundle from the registry",
		Long: `Download a bundle version from the Musher registry.

By default, bundles are cached in ~/.cache/musher/bundles/<namespace>/<slug>/<version>/.
Use --output-dir to extract to a specific directory instead (flat layout, no symlinks).

If no version is specified, the latest version is downloaded.

Authentication is attempted first; public bundles can be pulled without credentials.`,
		Example: `  musher pull acme/my-bundle
  musher pull acme/my-bundle:1.0.0
  musher pull acme/my-bundle:1.0.0 --output-dir ./bundles/
  musher pull acme/my-bundle --json`,
		Args: requireOneArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runPull(cmd, out, args[0], outputDir, force)
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", "",
		"Extract bundle to this directory instead of the default cache")
	cmd.Flags().BoolVar(&force, "force", false,
		"Re-download even if already cached")

	return cmd
}

// pullResult is the JSON output for the pull command.
type pullResult struct {
	Namespace  string `json:"namespace"`
	Slug       string `json:"slug"`
	Version    string `json:"version"`
	Dir        string `json:"dir"`
	Cached     bool   `json:"cached"`
	AssetCount int    `json:"assetCount,omitempty"`
}

func runPull(cmd *cobra.Command, out *output.Writer, ref, outputDir string, force bool) error {
	ctx := cmd.Context()

	namespace, slug, bundleVersion, err := parseBundleRefOptionalVersion(ref)
	if err != nil {
		return err
	}

	// Resolve version if not specified.
	if bundleVersion == "" {
		resolved, resolveErr := resolveLatestVersion(cmd, out, namespace, slug)
		if resolveErr != nil {
			return resolveErr
		}

		bundleVersion = resolved
	}

	versionRef := namespace + "/" + slug + ":" + bundleVersion

	// Determine target directory.
	targetDir := outputDir
	if targetDir == "" {
		cacheDir, cacheErr := paths.BundleCacheDir(namespace, slug, bundleVersion)
		if cacheErr != nil {
			return clierrors.Wrap(clierrors.ExitConfig, "Failed to determine cache directory", cacheErr)
		}

		targetDir = cacheDir

		// Check if already cached (skip when --force or --output-dir is set).
		if !force {
			if isCached(targetDir) {
				if out.JSON {
					if jsonErr := out.PrintJSON(pullResult{
						Namespace: namespace,
						Slug:      slug,
						Version:   bundleVersion,
						Dir:       targetDir,
						Cached:    true,
					}); jsonErr != nil {
						return fmt.Errorf("print JSON: %w", jsonErr)
					}

					return nil
				}

				out.Success("Already cached: %s", versionRef)
				out.Muted("  %s", targetDir)

				return nil
			}
		}
	}

	// Try authenticated client first, fall back to public.
	_, apiClient, authErr := newAPIClient()
	usePublic := authErr != nil

	if usePublic {
		cfg := configForPublicClient()
		apiClient = newPublicAPIClient(cfg)
	}

	// Pull the bundle.
	spin := out.Spinner("Pulling " + versionRef)
	spin.Start()

	var bundle *client.PullBundleResponse

	if usePublic {
		bundle, err = apiClient.PullPublicBundleVersion(ctx, namespace, slug, bundleVersion)
	} else {
		bundle, err = apiClient.PullBundleVersion(ctx, namespace, slug, bundleVersion)
		// Fall back to the public hub endpoint when the namespace endpoint
		// returns 403 (e.g. authenticated user pulling another user's public bundle).
		if err != nil {
			var httpErr *client.HTTPStatusError
			if errors.As(err, &httpErr) && httpErr.Status == http.StatusForbidden {
				cfg := configForPublicClient()
				pubClient := newPublicAPIClient(cfg)
				bundle, err = pubClient.PullPublicBundleVersion(ctx, namespace, slug, bundleVersion)
			}
		}
	}

	if err != nil {
		spin.StopWithFailure("Pull failed")
		return clierrors.PullFailed(err)
	}

	spin.StopWithSuccess("Pulled " + versionRef)

	// Write assets to target directory.
	if mkdirErr := safeio.MkdirAll(targetDir, 0o755); mkdirErr != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to create output directory", mkdirErr)
	}

	for _, asset := range bundle.Assets {
		assetPath := filepath.Join(targetDir, asset.LogicalPath)
		assetDir := filepath.Dir(assetPath)

		if mkErr := safeio.MkdirAll(assetDir, 0o755); mkErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral,
				"Failed to create directory for asset "+asset.LogicalPath, mkErr)
		}

		if writeErr := safeio.WriteFileAtomic(assetPath, []byte(asset.ContentText), 0o644); writeErr != nil {
			return clierrors.Wrap(clierrors.ExitGeneral,
				"Failed to write asset "+asset.LogicalPath, writeErr)
		}
	}

	if out.JSON {
		if jsonErr := out.PrintJSON(pullResult{
			Namespace:  namespace,
			Slug:       slug,
			Version:    bundleVersion,
			Dir:        targetDir,
			AssetCount: len(bundle.Assets),
		}); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	out.Success("Wrote %d asset(s) to %s", len(bundle.Assets), targetDir)

	return nil
}

// isCached returns true if the directory exists and contains at least one entry.
func isCached(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	entries, err := os.ReadDir(dir)

	return err == nil && len(entries) > 0
}

// resolveLatestVersion fetches bundle detail to determine the latest version.
func resolveLatestVersion(cmd *cobra.Command, out *output.Writer, namespace, slug string) (string, error) {
	_, apiClient, authErr := newAPIClient()
	if authErr != nil {
		cfg := configForPublicClient()
		apiClient = newPublicAPIClient(cfg)
	}

	spin := out.Spinner("Resolving latest version of " + namespace + "/" + slug)
	spin.Start()

	detail, err := apiClient.GetHubBundleDetail(cmd.Context(), namespace, slug)
	if err != nil {
		spin.StopWithFailure("Failed to resolve version")

		return "", clierrors.Wrap(clierrors.ExitGeneral,
			"Failed to resolve latest version for "+namespace+"/"+slug, err)
	}

	if detail.LatestVersion == "" {
		spin.StopWithFailure("No versions available")

		return "", clierrors.New(clierrors.ExitGeneral,
			"Bundle "+namespace+"/"+slug+" has no published versions")
	}

	spin.StopWithSuccess("Resolved " + namespace + "/" + slug + ":" + detail.LatestVersion)

	return detail.LatestVersion, nil
}
