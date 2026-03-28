package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
)

type removeOptions struct {
	all         bool
	interactive bool
	dryRun      bool
	yes         bool
}

func newBundleRemoveCmd() *cobra.Command {
	var opts removeOptions

	cmd := &cobra.Command{
		Use:   "remove [id-or-path...]",
		Short: "Remove assets from the bundle definition",
		Long: `Remove asset entries from musher.yaml.

With arguments: removes assets matching the given IDs or source paths.

Without arguments (on a TTY): shows an interactive picker of current
assets to select for removal.

Use --all to remove all assets at once.`,
		Example: `  musher bundle remove review
  musher bundle remove skills/review/SKILL.md agents/deploy.md
  musher bundle remove --all
  musher bundle remove --dry-run review`,
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runRemove(cmd, out, args, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.all, "all", false, "Remove all assets")
	cmd.Flags().BoolVarP(&opts.interactive, "interactive", "i", false, "Force interactive asset picker")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Preview changes without writing musher.yaml")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Accept removal without confirmation")

	return cmd
}

func runRemove(_ *cobra.Command, out *output.Writer, identifiers []string, opts removeOptions) error {
	workDir, err := bundledef.Resolve(".")
	if err != nil {
		return clierrors.New(clierrors.ExitUsage, "No musher.yaml or musher.yml found in this directory").
			WithHint("Run 'musher bundle init' to create one.")
	}

	// We need the directory, not the file path.
	workDir = filepath.Dir(workDir)

	def, err := bundledef.Load(workDir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to load bundle definition", err)
	}

	switch {
	case opts.all:
		return runRemoveAll(out, workDir, def, opts)
	case len(identifiers) > 0 && !opts.interactive:
		return runRemoveExplicit(out, workDir, identifiers, opts)
	default:
		return runRemoveInteractive(out, workDir, def, opts)
	}
}

func runRemoveExplicit(out *output.Writer, workDir string, identifiers []string, opts removeOptions) error {
	if opts.dryRun {
		preview, result, err := bundledef.RenderRemovePreview(workDir, identifiers)
		if err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to generate preview", err)
		}

		for _, asset := range result.Removed {
			out.Info("Would remove %s (%s)", asset.Src, asset.Kind)
		}

		for _, ident := range result.NotFound {
			out.Warning("Not found: %s", ident)
		}

		if len(result.Removed) > 0 {
			out.Println()
			out.Muted("Preview of musher.yaml:")
			out.Println(preview)
		}

		return nil
	}

	result, err := bundledef.RemoveAssets(workDir, identifiers)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to update musher.yaml", err)
	}

	for _, asset := range result.Removed {
		out.Success("Removed %s (%s)", asset.Src, asset.Kind)
	}

	for _, ident := range result.NotFound {
		out.Warning("Not found: %s", ident)
	}

	if len(result.Removed) > 0 {
		out.Println()
		out.Success("Removed %d asset(s) from musher.yaml", len(result.Removed))
	} else {
		out.Info("Nothing to remove.")
	}

	return nil
}

func runRemoveAll(out *output.Writer, workDir string, def *bundledef.Def, opts removeOptions) error {
	if len(def.Assets) == 0 {
		out.Info("No assets to remove.")
		return nil
	}

	// Always require confirmation for --all, even with --yes.
	if !opts.dryRun {
		out.Warning("This will remove all %d asset(s) from musher.yaml.", len(def.Assets))

		prompter := prompt.New(out)
		if prompter.CanPrompt() {
			ok, confirmErr := prompter.Confirm("Remove all assets?", false)
			if confirmErr != nil {
				return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read confirmation", confirmErr)
			}

			if !ok {
				out.Info("Canceled.")
				return nil
			}
		} else if !opts.yes {
			return clierrors.New(clierrors.ExitUsage, "Refusing to remove all assets without confirmation").
				WithHint("Use --yes to confirm in non-interactive mode.")
		}
	}

	if opts.dryRun {
		for _, asset := range def.Assets {
			out.Info("Would remove %s (%s)", asset.Src, asset.Kind)
		}

		out.Println()
		out.Info("Would remove all %d asset(s) from musher.yaml.", len(def.Assets))

		return nil
	}

	count, err := bundledef.RemoveAllAssets(workDir)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to update musher.yaml", err)
	}

	out.Success("Removed all %d asset(s) from musher.yaml", count)

	return nil
}

func runRemoveInteractive(out *output.Writer, workDir string, def *bundledef.Def, opts removeOptions) error {
	prompter := prompt.New(out)
	if !prompter.CanPrompt() {
		return clierrors.New(clierrors.ExitUsage, "Interactive mode requires a terminal").
			WithHint("Use 'musher bundle remove <id>' or 'musher bundle remove --all' in non-interactive mode.")
	}

	if len(def.Assets) == 0 {
		out.Info("No assets to remove.")
		return nil
	}

	// Build option labels.
	options := make([]string, len(def.Assets))
	for i, asset := range def.Assets {
		kind := asset.Kind
		if kind == "" {
			kind = bundledef.InferKind(asset.Src)
		}

		options[i] = fmt.Sprintf("%s — %s (%s)", asset.ID, asset.Src, kind)
	}

	indices, err := prompter.SelectMultiple("Select assets to remove:", options)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to read selection", err)
	}

	if len(indices) == 0 {
		out.Info("Nothing selected.")
		return nil
	}

	var identifiers []string
	for _, idx := range indices {
		identifiers = append(identifiers, def.Assets[idx].ID)
	}

	return runRemoveExplicit(out, workDir, identifiers, opts)
}
