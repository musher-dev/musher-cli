package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/importer"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newImportSkillsCmd() *cobra.Command {
	var (
		force  bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "skills <path...>",
		Short: "Import skills from local directories",
		Long: `Import agent skills from local directories into the bundle workspace.

Each path can be:
  - A SKILL.md file (uses its parent directory)
  - A directory containing SKILL.md
  - A directory containing subdirectories with SKILL.md files`,
		Example: `  musher import skills ./my-skills/code-review/
  musher import skills ./skills/
  musher import skills ./my-skill/SKILL.md
  musher import skills ./dir1 ./dir2 --force`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())
			return runImportSkills(out, args, force, dryRun)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing skills")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without modifying files")

	return cmd
}

func runImportSkills(out *output.Writer, paths []string, force, dryRun bool) error {
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

	discovered, warnings, err := importer.ScanDirs(paths)
	if err != nil {
		return clierrors.ImportFailed(err)
	}

	for _, w := range warnings {
		out.Warning("%s", w)
	}

	if len(discovered) == 0 {
		out.Warning("No skills discovered")
		return nil
	}

	opts := importer.Options{
		BundleRoot: workDir,
		Force:      force,
		DryRun:     dryRun,
	}

	results := importer.Run(opts, discovered, def)

	return renderImportResults(out, results, def, workDir, dryRun)
}

func renderImportResults(out *output.Writer, results []importer.ImportResult, def *bundledef.Def, workDir string, dryRun bool) error {
	if out.JSON {
		if jsonErr := out.PrintJSON(results); jsonErr != nil {
			return fmt.Errorf("print JSON: %w", jsonErr)
		}

		return nil
	}

	var imported, skipped, errored int

	prefix := ""
	if dryRun {
		prefix = "[dry-run] "
	}

	for i := range results {
		r := &results[i]

		switch {
		case r.Err != nil:
			out.Failure("%sError    %s: %v", prefix, r.Skill.Name, r.Err)
			errored++
		case r.Skipped:
			out.Warning("%sSkipped  %s: %s", prefix, r.Skill.Name, r.SkipReason)
			skipped++
		case r.Imported:
			out.Success("%sImported %s from %s", prefix, r.Skill.Name, r.Skill.Provenance)
			imported++
		}

		for _, w := range r.Warnings {
			out.Warning("  %s", w)
		}
	}

	out.Println()
	out.Print("%sImported %d skills", prefix, imported)

	if skipped > 0 {
		out.Print(", skipped %d", skipped)
	}

	if errored > 0 {
		out.Print(", %d errors", errored)
	}

	out.Print("\n")

	if errored > 0 {
		return clierrors.ImportFailed(fmt.Errorf("%d skills failed to import", errored))
	}

	// Save the updated bundle definition (unless dry-run).
	if !dryRun && imported > 0 {
		if err := bundledef.Save(workDir, def); err != nil {
			return clierrors.Wrap(clierrors.ExitGeneral, "Failed to save musher.yaml", err)
		}
	}

	return nil
}
