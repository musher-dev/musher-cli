package main

import (
	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/doctor"
	"github.com/musher-dev/musher-cli/internal/output"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common issues",
		Long: `Run diagnostic checks to identify configuration and connectivity issues.

Checks performed:
  - Directory structure and permissions
  - Configuration file validity
  - Credential file security
  - API connectivity and response time
  - Authentication status
  - CLI version`,
		Example: `  musher doctor`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := output.FromContext(cmd.Context())

			out.Println("Musher Doctor")
			out.Println("=============")
			out.Println()

			runner := doctor.New()
			results := runner.Run(cmd.Context())

			doctor.RenderResults(results, out.Print, out.Success, out.Warning, out.Failure, out.Muted)

			reportRetiredConfigKeys(cmd, out)

			passed, failed, warnings := doctor.Summary(results)

			out.Println()
			out.Print("%d passed", passed)

			if failed > 0 {
				out.Print(", %d failed", failed)
			}

			if warnings > 0 {
				out.Print(", %d warning(s)", warnings)
			}

			out.Println()

			return nil
		},
	}
}

// reportRetiredConfigKeys tells the user when config.yaml still holds keys this
// version no longer reads, so their removal is explained rather than silent.
func reportRetiredConfigKeys(cmd *cobra.Command, out *output.Writer) {
	cfg := config.FromContext(cmd.Context())

	stale := cfg.UnrecognizedKeys()
	if len(stale) == 0 {
		return
	}

	out.Println()
	out.Warning("Your config contains %d key(s) this version no longer uses:", len(stale))

	for _, key := range stale {
		if reason, ok := config.RetiredKeyReason(key); ok {
			out.Muted("  %s — %s", key, reason)
		} else {
			out.Muted("  %s", key)
		}
	}

	out.Muted("  These are ignored and safe to delete.")
}
