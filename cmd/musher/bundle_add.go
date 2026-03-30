package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

type addOptions struct {
	all         bool
	interactive bool
	dryRun      bool
	yes         bool
	kind        string
	id          string
}

func newBundleAddCmd() *cobra.Command {
	var opts addOptions

	cmd := &cobra.Command{
		Use:   "add [path...]",
		Short: "Add assets to the bundle definition",
		Long: `Register asset files in musher.yaml.

With paths: adds the specified files as assets, inferring kind and id from
the path. Use --kind and --id to override inference.

Without paths (on a TTY): shows an interactive picker of discoverable
assets not yet tracked in musher.yaml.

Use --all to add all discoverable conventional assets at once.`,
		Example: `  musher bundle add skills/review/SKILL.md
  musher bundle add agents/reviewer.md --kind agent --id reviewer
  musher bundle add "skills/*/SKILL.md"
  musher bundle add "agents/*.md" "prompts/*.md"
  musher bundle add --all
  musher bundle add --dry-run
  musher bundle add`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runAdd(cmd, out, args, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.all, "all", false, "Add all discoverable conventional assets")
	cmd.Flags().BoolVarP(&opts.interactive, "interactive", "i", false, "Force interactive asset picker")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Preview changes without writing musher.yaml")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Accept inferred defaults without confirmation")
	cmd.Flags().StringVar(&opts.kind, "kind", "", "Override asset kind (skill, agent, prompt, tool, config)")
	cmd.Flags().StringVar(&opts.id, "id", "", "Override asset ID")

	return cmd
}

func runAdd(_ *cobra.Command, out *output.Writer, paths []string, opts addOptions) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	// Require musher.yaml or musher.yml to exist.
	if _, resolveErr := bundledef.Resolve(workDir); resolveErr != nil {
		return clierrors.New(clierrors.ExitUsage, "No musher.yaml or musher.yml found in this directory").
			WithHint("Run 'musher bundle init' to create one.")
	}

	def, err := bundledef.Load(workDir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to load bundle definition", err)
	}

	// Expand any glob patterns in paths before dispatching.
	if len(paths) > 0 {
		expanded, expandErr := expandGlobs(workDir, paths)
		if expandErr != nil {
			return expandErr
		}

		paths = expanded
	}

	switch {
	case len(paths) > 0 && !opts.interactive:
		return runAddExplicit(out, workDir, def, paths, opts)
	case opts.all:
		return runAddAll(out, workDir, def, opts)
	default:
		return runAddInteractive(out, workDir, def, opts)
	}
}

func runAddExplicit(out *output.Writer, workDir string, def *bundledef.Def, paths []string, opts addOptions) error {
	// --kind and --id only valid with exactly one path.
	if len(paths) > 1 && (opts.kind != "" || opts.id != "") {
		return clierrors.New(clierrors.ExitUsage, "--kind and --id can only be used with a single path")
	}

	trackedSrc := makeTrackedSrcSet(def)
	trackedID := makeTrackedIDMap(def)

	var assets []bundledef.Asset

	for _, path := range paths {
		src, resolveErr := resolveAssetPath(workDir, path)
		if resolveErr != nil {
			return resolveErr
		}

		if trackedSrc[src] {
			out.Muted("Skipped %s (already tracked)", src)
			continue
		}

		assetID := opts.id
		if assetID == "" {
			assetID = bundledef.InferID(src)
		}

		// Check for ID conflict.
		if existingSrc, ok := trackedID[assetID]; ok && existingSrc != src {
			return clierrors.New(clierrors.ExitUsage,
				fmt.Sprintf("ID %q is already used by %s", assetID, existingSrc)).
				WithHint("Use --id to assign a different ID.")
		}

		kind := opts.kind
		if kind == "" {
			kind = bundledef.InferKind(src)
		}

		assets = append(assets, bundledef.Asset{
			ID:   assetID,
			Src:  src,
			Kind: kind,
		})
	}

	if len(assets) == 0 {
		out.Info("Nothing to add.")
		return nil
	}

	return appendAndReport(out, workDir, assets, opts)
}

func runAddAll(out *output.Writer, workDir string, def *bundledef.Def, opts addOptions) error {
	discovered, err := bundledef.DiscoverAssets(workDir, def)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to discover assets", err)
	}

	if len(discovered) == 0 {
		out.Info("No untracked assets found.")
		out.Muted("Place assets in skills/, agents/, prompts/, tools/, or configs/ directories.")

		return nil
	}

	assets := discoveredToAssets(discovered)

	if !opts.yes && !opts.dryRun {
		out.Info("Found %d asset(s) to add:", len(assets))

		for _, asset := range assets {
			out.Info("  %s (%s)", asset.Src, asset.Kind)
		}

		out.Println()

		prompter := prompt.New(out)
		if prompter.CanPrompt() {
			ok, confirmErr := prompter.Confirm("Add these assets?", true)
			if confirmErr != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read confirmation", confirmErr)
			}

			if !ok {
				out.Info("Canceled.")
				return nil
			}
		}
	}

	return appendAndReport(out, workDir, assets, opts)
}

func runAddInteractive(out *output.Writer, workDir string, def *bundledef.Def, opts addOptions) error {
	prompter := prompt.New(out)
	if !prompter.CanPrompt() {
		return clierrors.New(clierrors.ExitUsage, "Interactive mode requires a terminal").
			WithHint("Use 'musher bundle add <path>' or 'musher bundle add --all' in non-interactive mode.")
	}

	discovered, err := bundledef.DiscoverAssets(workDir, def)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to discover assets", err)
	}

	if len(discovered) == 0 {
		out.Info("No untracked assets found.")
		out.Muted("Place assets in skills/, agents/, prompts/, tools/, or configs/ directories.")

		return nil
	}

	// Build option labels.
	options := make([]string, len(discovered))
	for i, disc := range discovered {
		label := fmt.Sprintf("%s (%s)", disc.Src, disc.Kind)
		if disc.IDConflict {
			label += " [ID conflict]"
		}

		options[i] = label
	}

	indices, err := prompter.SelectMultiple("Select assets to add:", options)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read selection", err)
	}

	var assets []bundledef.Asset
	for _, idx := range indices {
		assets = append(assets, discovered[idx].Asset)
	}

	if len(assets) == 0 {
		out.Info("Nothing selected.")
		return nil
	}

	return appendAndReport(out, workDir, assets, opts)
}

func appendAndReport(out *output.Writer, workDir string, assets []bundledef.Asset, opts addOptions) error {
	if opts.dryRun {
		preview, err := bundledef.RenderAppendPreview(workDir, assets)
		if err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to generate preview", err)
		}

		for _, asset := range assets {
			out.Info("Would add %s (%s)", asset.Src, asset.Kind)
		}

		out.Println()
		out.Muted("Preview of musher.yaml:")
		out.Println(preview)

		return nil
	}

	count, err := bundledef.AppendAssets(workDir, assets)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to update musher.yaml", err)
	}

	for _, asset := range assets {
		out.Success("Added %s (%s)", asset.Src, asset.Kind)
	}

	if count > 0 {
		out.Println()
		out.Success("Added %d asset(s) to musher.yaml", count)
	}

	return nil
}

// resolveAssetPath validates and normalizes a user-provided path to a relative
// src path from the working directory.
func resolveAssetPath(workDir, assetPath string) (string, error) {
	// Make absolute if relative.
	absPath := assetPath
	if !filepath.IsAbs(assetPath) {
		absPath = filepath.Join(workDir, assetPath)
	}

	// Verify the file exists.
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", clierrors.New(clierrors.ExitUsage, "File not found: "+assetPath)
		}

		return "", clierrors.Wrap(clierrors.ExitGeneral, "Cannot access: "+assetPath, err)
	}

	if info.IsDir() {
		return "", clierrors.New(clierrors.ExitUsage, "Path is a directory, not a file: "+assetPath)
	}

	// Make relative to workDir.
	rel, err := filepath.Rel(workDir, absPath)
	if err != nil {
		return "", clierrors.Wrap(clierrors.ExitGeneral, "Failed to compute relative path", err)
	}

	// Normalize to forward slashes.
	return filepath.ToSlash(rel), nil
}

func makeTrackedSrcSet(def *bundledef.Def) map[string]bool {
	tracked := make(map[string]bool, len(def.Assets))
	for _, asset := range def.Assets {
		tracked[filepath.ToSlash(asset.Src)] = true
	}

	return tracked
}

func makeTrackedIDMap(def *bundledef.Def) map[string]string {
	tracked := make(map[string]string, len(def.Assets))
	for _, asset := range def.Assets {
		tracked[asset.ID] = filepath.ToSlash(asset.Src)
	}

	return tracked
}

func discoveredToAssets(discovered []bundledef.DiscoveredAsset) []bundledef.Asset {
	assets := make([]bundledef.Asset, len(discovered))
	for i, disc := range discovered {
		assets[i] = disc.Asset
	}

	return assets
}
