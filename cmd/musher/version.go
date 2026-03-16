package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/output"
)

// VersionInfo represents version information for JSON output.
type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Show version information",
		Long:    `Display the musher binary version, git commit, and build date.`,
		Example: `  musher version`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			if out.JSON {
				return out.PrintJSON(VersionInfo{
					Version: version,
					Commit:  commit,
					Date:    date,
				})
			}

			out.Print("musher %s\n", version)
			out.Print("  commit: %s\n", commit)
			out.Print("  built:  %s\n", date)

			return nil
		},
	}
}
