package main

import (
	"github.com/spf13/cobra"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import agent skills from external sources",
		Long: `Import agent skills from npm packages or local directories into
your Musher bundle workspace. Discovered skills are copied into the skills/
directory and registered in musher.yaml.

No external code is executed — only SKILL.md files and supporting assets
are read and copied.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newImportNpmCmd(),
		newImportSkillsCmd(),
	)

	return cmd
}
