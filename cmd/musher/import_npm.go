package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/importer"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newImportNpmCmd() *cobra.Command {
	var (
		installed bool
		force     bool
		dryRun    bool
	)

	cmd := &cobra.Command{
		Use:   "npm --installed [flags]",
		Short: "Import skills from npm packages",
		Long: `Import agent skills from npm packages into the bundle workspace.

Use --installed to scan the local node_modules/ directory for packages
that contain agent skills. Skills are discovered via:
  1. package.json "agents.skills" field (explicit paths)
  2. skills/*/SKILL.md directories (convention)
  3. SKILL.md at package root (single-skill packages)

No npm code is executed — only files are read and copied.`,
		Example: `  musher import npm --installed
  musher import npm --installed --force
  musher import npm --installed --dry-run`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if len(args) > 0 {
				return &clierrors.CLIError{
					Message: "Fetching packages from the npm registry is not yet supported",
					Hint:    "Use --installed to scan local node_modules/ instead",
					Code:    clierrors.ExitUsage,
				}
			}

			if !installed {
				return &clierrors.CLIError{
					Message: "No source specified",
					Hint:    "Use --installed to scan local node_modules/",
					Code:    clierrors.ExitUsage,
				}
			}

			return runImportNpm(out, force, dryRun)
		},
	}

	cmd.Flags().BoolVar(&installed, "installed", false, "Scan local node_modules/ for skills")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing skills")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without modifying files")

	return cmd
}

func runImportNpm(out *output.Writer, force, dryRun bool) error {
	workDir, err := os.Getwd()
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to determine working directory", err)
	}

	def, err := bundledef.Load(workDir)
	if err != nil {
		return &clierrors.CLIError{
			Message: "No musher.yaml found",
			Hint:    "Run 'musher init' first",
			Code:    clierrors.ExitConfig,
		}
	}

	spin := out.Spinner("Scanning node_modules")
	spin.Start()

	discovered, warnings, err := importer.ScanNodeModules(workDir)
	if err != nil {
		spin.StopWithFailure("Scan failed")
		return clierrors.ImportFailed(err)
	}

	spin.Stop()

	for _, w := range warnings {
		out.Warning("%s", w)
	}

	if len(discovered) == 0 {
		out.Warning("No skills found in node_modules/")
		return nil
	}

	out.Info("Found %d skills in node_modules/", len(discovered))

	opts := importer.Options{
		BundleRoot: workDir,
		Force:      force,
		DryRun:     dryRun,
	}

	results := importer.Run(opts, discovered, def)

	return renderImportResults(out, results, def, workDir, dryRun)
}
