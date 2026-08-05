package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/client/stream"
	"github.com/musher-dev/musher-cli/internal/config"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
)

// deploymentReader is the slice of the platform API the read commands use.
//
// It is declared here, consumer-side, for the same reason workflow.DeployAPI
// is: cmd/ may not import net/http, so a command has to be testable against a
// fake rather than against an HTTP server. *client.Client satisfies it.
type deploymentReader interface {
	ListOrganizations(ctx context.Context) ([]client.Organization, error)
	ListEnvironments(ctx context.Context, orgID, status string) ([]client.Environment, error)
	ListDeployments(
		ctx context.Context, orgID string, limit int, cursor string,
	) (*client.Page[client.Deployment], error)
	GetDeploymentByName(ctx context.Context, orgID, name string) (*client.Deployment, error)
	ListEndpoints(ctx context.Context, orgID, deploymentID string) ([]client.Endpoint, error)
	ListReplicas(ctx context.Context, deploymentID string) ([]client.ReplicaGroup, error)
	ListLogEntries(
		ctx context.Context, deploymentID, logStream string, limit int,
	) (*client.Page[client.LogEntry], error)
	DeploymentLogs(deploymentID string) (stream.Minter, stream.Opener)
}

// listPageSize is how many deployments one `musher list` fetches. It is a
// screenful and then some; --limit raises it.
const listPageSize = 50

// emptyCell is what a table prints when the platform sent nothing.
const emptyCell = "-"

func newListCmd() *cobra.Command {
	var (
		limit int
		all   bool
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List deployments",
		Long: `List the deployments in the active organization.

The table goes to stdout so it can be piped; use --json for a machine-readable
array.`,
		Example: `  musher list
  musher ls --json | jq -r '.[] | select(.phase != "ACTIVE") | .name'`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd, limit, all)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", listPageSize, "Maximum number of deployments to show")
	cmd.Flags().BoolVar(&all, "all", false, "Include deployments in every environment")

	return cmd
}

// deploymentSummary is one row of `musher list`.
type deploymentSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Phase       string `json:"phase"`
	Health      string `json:"health"`
	Readiness   string `json:"readiness"`
	Environment string `json:"environment"`
	CreatedAt   string `json:"createdAt"`
	Age         string `json:"age"`
}

func runList(cmd *cobra.Command, limit int, all bool) error {
	out := output.FromContext(cmd.Context())

	api, err := requireAuthFromContext(cmd.Context())
	if err != nil {
		return err
	}

	orgID, err := resolveOrgID(cmd.Context(), api)
	if err != nil {
		return err
	}

	page, err := api.ListDeployments(cmd.Context(), orgID, limit, "")
	if err != nil {
		return apiFailure("Could not list deployments", err)
	}

	rows := summarize(cmd.Context(), api, orgID, page.Data, all)

	if out.JSON {
		return writeJSONLine(out, rows)
	}

	renderDeploymentTable(out, rows)

	return nil
}

// summarize turns deployments into rows, naming environments where it can.
func summarize(
	ctx context.Context,
	api deploymentReader,
	orgID string,
	deployments []client.Deployment,
	all bool,
) []deploymentSummary {
	names := environmentNames(ctx, api, orgID)
	now := time.Now()
	rows := make([]deploymentSummary, 0, len(deployments))

	for index := range deployments {
		deployment := &deployments[index]

		environment := names[deployment.Spec.EnvironmentID]
		if environment == "" {
			environment = emptyCell
		}

		if !all && deployment.Status.Phase == "DELETED" {
			continue
		}

		rows = append(rows, deploymentSummary{
			ID:          deployment.Metadata.ID,
			Name:        deployment.Metadata.Name,
			Phase:       orDash(deployment.Status.Phase),
			Health:      orDash(deployment.Status.Health),
			Readiness:   orDash(deployment.Status.Readiness),
			Environment: environment,
			CreatedAt:   formatTimestamp(deployment.Metadata.CreatedAt.Time),
			Age:         relativeAge(deployment.Metadata.CreatedAt.Time, now),
		})
	}

	return rows
}

// environmentNames maps environment ids onto labels, best effort: a listing
// that cannot name environments is still worth printing.
func environmentNames(ctx context.Context, api deploymentReader, orgID string) map[string]string {
	environments, err := api.ListEnvironments(ctx, orgID, "")
	if err != nil {
		return map[string]string{}
	}

	names := make(map[string]string, len(environments))
	for index := range environments {
		names[environments[index].ID] = environments[index].Label()
	}

	return names
}

// renderDeploymentTable writes the human table to stdout.
func renderDeploymentTable(out *output.Writer, rows []deploymentSummary) {
	if len(rows) == 0 {
		out.Muted("No deployments yet. Create one with 'musher deploy'.")

		return
	}

	writer := tabwriter.NewWriter(out.Out, 0, 0, 2, ' ', 0)

	fmt.Fprintln(writer, "NAME\tPHASE\tHEALTH\tREADY\tENVIRONMENT\tAGE")

	for index := range rows {
		row := &rows[index]
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
			row.Name, row.Phase, row.Health, row.Readiness, row.Environment, row.Age)
	}

	_ = writer.Flush() //nolint:errcheck // a failed stdout flush is reported by the shell, not by us
}

// resolveOrgID picks the organization every read command acts in.
//
// The credential's first organization is the answer for the overwhelmingly
// common single-org case; MUSHER_ORG or `musher config set context.organization`
// selects among several.
func resolveOrgID(ctx context.Context, api deploymentReader) (string, error) {
	return resolveOrgIDFor(ctx, api, orgPreference(ctx))
}

// resolveOrgIDFor is resolveOrgID with the preference supplied explicitly.
func resolveOrgIDFor(ctx context.Context, api deploymentReader, preference string) (string, error) {
	wanted := strings.TrimSpace(preference)

	orgs, err := api.ListOrganizations(ctx)
	if err != nil {
		return "", apiFailure("Could not resolve the organization", err)
	}

	if len(orgs) == 0 {
		return "", &clierrors.CLIError{
			Message: "This credential can act in no organization",
			Hint:    "Check the key with 'musher auth status'",
			Code:    clierrors.ExitPermission,
		}
	}

	if wanted == "" {
		return orgs[0].ID, nil
	}

	for _, org := range orgs {
		if strings.EqualFold(org.ID, wanted) ||
			strings.EqualFold(org.Handle, wanted) ||
			strings.EqualFold(org.Name, wanted) {
			return org.ID, nil
		}
	}

	return "", &clierrors.CLIError{
		Message: "Organization " + wanted + " is not visible to this credential",
		Hint:    "List what the credential can see with 'musher auth status'",
		Code:    clierrors.ExitConfig,
	}
}

// orgPreference returns the configured organization selector, or "".
func orgPreference(ctx context.Context) string {
	return config.FromContext(ctx).Organization()
}

// writeJSONLine writes one compact JSON value to stdout, and nothing else.
func writeJSONLine(out *output.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Failed to encode the result", err)
	}

	fmt.Fprintln(out.Out, string(encoded))

	return nil
}

// orDash renders an empty server value as a table dash.
func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return emptyCell
	}

	return value
}

// formatTimestamp renders a timestamp as RFC 3339, or "" when unknown.
func formatTimestamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}

	return at.UTC().Format(time.RFC3339)
}
