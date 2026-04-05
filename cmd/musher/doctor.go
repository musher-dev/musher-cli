package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/doctor"
	"github.com/musher-dev/musher-cli/internal/harness"
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
  - CLI version
  - Harness provider availability`,
		Example: `  musher doctor`,
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := output.FromContext(cmd.Context())

			out.Println("Musher Doctor")
			out.Println("=============")
			out.Println()

			// Run core diagnostics.
			runner := doctor.New()
			coreResults := runner.Run(cmd.Context())

			doctor.RenderResults(coreResults, out.Print, out.Success, out.Warning, out.Failure, out.Muted)

			// Run harness provider checks.
			out.Println()
			out.Println("Harness Providers")
			out.Println("-----------------")
			out.Println()

			reg := newHarnessRegistry()
			harnessResults := runHarnessChecks(cmd.Context(), reg)

			doctor.RenderResults(harnessResults, out.Print, out.Success, out.Warning, out.Failure, out.Muted)

			// Combined summary.
			allResults := make([]doctor.Result, 0, len(coreResults)+len(harnessResults))
			allResults = append(allResults, coreResults...)
			allResults = append(allResults, harnessResults...)
			passed, failed, warnings := doctor.Summary(allResults)

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

// runHarnessChecks converts harness health reports into doctor results.
func runHarnessChecks(ctx context.Context, reg *harness.Registry) []doctor.Result {
	reports := harness.CheckAllHealth(ctx, reg)

	var results []doctor.Result

	for _, report := range reports {
		for _, check := range report.Checks {
			name := fmt.Sprintf("%s / %s", report.DisplayName, check.Name)

			var status doctor.Status

			switch check.Status {
			case harness.CheckPass:
				status = doctor.StatusPass
			case harness.CheckWarn:
				status = doctor.StatusWarn
			case harness.CheckFail:
				status = doctor.StatusFail
			}

			r := doctor.Result{
				Name:    name,
				Status:  status,
				Message: check.Message,
			}

			// Add install hint for binary failures.
			if check.Status == harness.CheckFail && check.Name == "binary" {
				if hint := findInstallHint(reg, report.ProviderName); hint != "" {
					r.Detail = hint
				}
			}

			results = append(results, r)
		}
	}

	return results
}

// findInstallHint returns the install hint for a provider, if configured.
func findInstallHint(reg *harness.Registry, name string) string {
	prov, ok := reg.Get(name)
	if !ok {
		return ""
	}

	return prov.Spec.Status.InstallHint
}
