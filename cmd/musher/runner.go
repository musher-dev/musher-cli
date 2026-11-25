package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/observability"
	"github.com/musher-dev/musher-cli/internal/runner"
)

// cmdNameRunner is the runner-mode command tree, which the root's hooks treat
// apart: no config file, no keyring, no update check.
const cmdNameRunner = "runner"

// isRunnerMode reports whether cmd is `musher runner` or under it.
func isRunnerMode(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == cmdNameRunner && c.Parent() == cmd.Root() {
			return true
		}
	}

	return false
}

func newRunnerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdNameRunner,
		Short: "Runner mode: fetch and build inside an isolation guest",
		Long: `Runner mode is the CLI inside an isolation guest, started by the host agent
for one attempt of a platform-owned run. It authenticates only with the run
attempt token in MUSHER_API_KEY and calls only the /runner/v1 surface. It
reads no config file, profile or keyring, and never checks for updates.

Exit codes: 0 success reported, 20 failure reported, 21 fenced (the API no
longer accepts the attempt), 22 deadline reached, 23 outcome not reported,
4 missing environment, 64 usage. self-test exits 0 whenever it wrote its
results, failed checks included.`,
		Hidden: true,
		Args:   noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		Annotations: map[string]string{
			"musher.dev/effect":     "write",
			"musher.dev/operations": "registry:push",
		},
	}

	cmd.AddCommand(newRunnerFetchSourceCmd(), newRunnerBuildCmd(), newRunnerSelfTestCmd())

	return cmd
}

func newRunnerFetchSourceCmd() *cobra.Command {
	var snapshot string

	cmd := &cobra.Command{
		Use:   "fetch-source",
		Short: "Fetch a source snapshot's commit and store its archive",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			environ, err := runner.LoadEnvironment()
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			id, err := runner.ResolveID(snapshot, environ.Inputs.SnapshotID, "snapshot")
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			fetcher, err := runner.NewFetcher(environ, observability.FromContext(cmd.Context()))
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			return fetcher.Run(cmd.Context(), id, environ.ResultsFile)
		},
	}

	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Source snapshot id (default: snapshot_id in MUSHER_INPUTS_FILE)")

	return cmd
}

func newRunnerBuildCmd() *cobra.Command {
	var buildID, buildkitHost string

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build an image build's recipe and push the image",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			environ, err := runner.LoadEnvironment()
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			id, err := runner.ResolveID(buildID, environ.Inputs.BuildID, "build")
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			builder, err := runner.NewBuilder(environ, runnerBuildKitHost(buildkitHost), cmd.ErrOrStderr(), observability.FromContext(cmd.Context()))
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			return builder.Run(cmd.Context(), id, environ.ResultsFile)
		},
	}

	cmd.Flags().StringVar(&buildID, "build", "", "Image build id (default: build_id in MUSHER_INPUTS_FILE)")
	addBuildKitHostFlag(cmd, &buildkitHost)

	return cmd
}

func newRunnerSelfTestCmd() *cobra.Command {
	var (
		buildkitHost, registryHost, resultsFile string
		denyMetadata, denyPrivate, denyCGNAT    string
	)

	cmd := &cobra.Command{
		Use:   "self-test",
		Short: "Check the guest can build: BuildKit, egress, scratch",
		Long: `Run the in-guest half of the host canary and write the results document
(docs/runner-mode.md) to --results-file, MUSHER_RESULTS_FILE, and stdout.
Exits 0 whenever the results were written, even when a check failed: the
verdicts are in the document. --api-url (or MUSHER_API_URL) is the API whose
egress is proved.`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			environ, err := runner.LoadEnvironment()
			if err != nil {
				return err //nolint:wrapcheck // already a CLI error
			}

			if registryHost == "" {
				registryHost = env.Get(env.RegistryHost)
			}

			// --api-url is the root's persistent flag; in runner mode it only
			// names the API this check proves egress to.
			if apiURL, flagErr := cmd.Flags().GetString("api-url"); flagErr == nil && strings.TrimSpace(apiURL) != "" {
				environ.APIURL = strings.TrimSpace(apiURL)
			}

			test := runner.NewSelfTest(environ, runnerBuildKitHost(buildkitHost), strings.TrimSpace(registryHost))
			test.DenyMetadata, test.DenyPrivate, test.DenyCGNAT = denyMetadata, denyPrivate, denyCGNAT

			res := test.Run(cmd.Context())

			data, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return clierrors.Wrap(clierrors.ExitExecution, "Cannot encode the self-test results", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), string(data))

			if resultsFile == "" {
				resultsFile = environ.ResultsFile
			}

			return runner.WriteSelfTest(resultsFile, res)
		},
	}

	set := cmd.Flags()
	addBuildKitHostFlag(cmd, &buildkitHost)
	set.StringVar(&registryHost, "registry", "", "Registry host[:port] to prove TCP+TLS to (or set MUSHER_REGISTRY_HOST)")
	set.StringVar(&resultsFile, "results-file", "", "Where to write the results (default: MUSHER_RESULTS_FILE)")
	set.StringVar(&denyMetadata, "deny-metadata", runner.DefaultDenyMetadata, "Metadata endpoint that must be unreachable")
	set.StringVar(&denyPrivate, "deny-rfc1918", runner.DefaultDenyPrivate, "RFC 1918 address that must be unreachable")
	set.StringVar(&denyCGNAT, "deny-cgnat", runner.DefaultDenyCGNAT, "RFC 6598 (100.64.0.0/10) address that must be unreachable")

	return cmd
}

func addBuildKitHostFlag(cmd *cobra.Command, target *string) {
	cmd.Flags().StringVar(target, "buildkit-host", "", "buildkitd address (or set MUSHER_BUILDKIT_HOST; default "+runner.DefaultBuildKitHost+")")
}

// runnerBuildKitHost is the flag, then MUSHER_BUILDKIT_HOST, then the guest's
// socket. The generic BUILDKIT_HOST and the Docker Engine are never used here.
func runnerBuildKitHost(flag string) string {
	if v := strings.TrimSpace(flag); v != "" {
		return v
	}

	return env.GetDefault(env.BuildKitHost, runner.DefaultBuildKitHost)
}
