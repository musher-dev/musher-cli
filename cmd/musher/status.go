package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// statusPollInterval is how often `musher status --watch` re-reads. It is
// deliberately the same as the deploy watcher's first rung, so the two commands
// feel like one thing.
const statusPollInterval = 3 * time.Second

// statusNotFound is HTTP 404, spelled out because cmd/ may not import net/http:
// HTTP is internal/client's business, and this is the one status code the
// command layer has to recognize to give better advice than "request failed".
const statusNotFound = 404

// statusFlags holds the parsed command line for `musher status`.
type statusFlags struct {
	watch      bool
	replicas   bool
	conditions bool
	timeout    time.Duration
}

func newStatusCmd() *cobra.Command {
	flags := &statusFlags{}

	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show the state of a deployment",
		Long: `Show the phase, health, readiness and URL of a deployment.

With no name, the deployment named in ./musher.yaml is used.`,
		Example: `  musher status api
  musher status api --watch
  musher status api --replicas --conditions
  musher status api --json | jq -r '.url'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, args, flags)
		},
	}

	cmd.Flags().BoolVar(&flags.watch, "watch", false, "Follow the deployment until it settles")
	cmd.Flags().BoolVar(&flags.replicas, "replicas", false, "Include per-replica detail")
	cmd.Flags().BoolVar(&flags.conditions, "conditions", false, "Include the status conditions")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", 15*time.Minute, "How long --watch waits before giving up")

	return cmd
}

// statusPayload is the machine-readable answer of `musher status`.
type statusPayload struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	URL         *string             `json:"url"`
	Phase       string              `json:"phase"`
	Health      string              `json:"health"`
	Readiness   string              `json:"readiness"`
	Reason      string              `json:"reason"`
	ConfigStale bool                `json:"isConfigStale"`
	CreatedAt   string              `json:"createdAt"`
	UpdatedAt   string              `json:"updatedAt"`
	Conditions  []conditionPayload  `json:"conditions"`
	Replicas    []replicaPayload    `json:"replicas"`
	Endpoints   []endpointPayload   `json:"endpoints"`
	Error       *deployErrorPayload `json:"error"`
}

type conditionPayload struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type replicaPayload struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Health    string `json:"health"`
	Restarts  int    `json:"restarts"`
	Age       string `json:"age"`
}

type endpointPayload struct {
	URL        string `json:"url"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"containerPort"`
	Visibility string `json:"visibility"`
	State      string `json:"state"`
}

func runStatus(cmd *cobra.Command, args []string, flags *statusFlags) error {
	out := output.FromContext(cmd.Context())

	name, err := resolveDeploymentName(args)
	if err != nil {
		return err
	}

	api, err := requireAuthFromContext(cmd.Context())
	if err != nil {
		return err
	}

	orgID, err := resolveOrgID(cmd.Context(), api)
	if err != nil {
		return err
	}

	deployment, err := readDeployment(cmd.Context(), api, orgID, name)
	if err != nil {
		return err
	}

	if flags.watch {
		deployment, err = watchStatus(cmd.Context(), api, orgID, deployment, flags.timeout, out)
		if err != nil {
			return err
		}
	}

	return renderStatus(cmd.Context(), api, orgID, deployment, flags, out)
}

// readDeployment resolves a deployment by name, turning a 404 into advice.
func readDeployment(
	ctx context.Context,
	api deploymentReader,
	orgID, name string,
) (*client.Deployment, error) {
	deployment, err := api.GetDeploymentByName(ctx, orgID, name)
	if err == nil {
		return deployment, nil
	}

	var statusErr *client.HTTPStatusError
	if errors.As(err, &statusErr) && statusErr.Status == statusNotFound {
		return nil, &clierrors.CLIError{
			Message: "No deployment named " + name,
			Hint:    "See what exists with 'musher list'",
			Code:    clierrors.ExitGeneral,
		}
	}

	return nil, apiFailure("Could not read the deployment", err)
}

// watchStatus follows a deployment until it settles, narrating to stderr.
//
// It settles with the same predicate `musher deploy` uses, so the two commands
// can never disagree about whether a deployment is done.
func watchStatus(
	ctx context.Context,
	api deploymentReader,
	orgID string,
	deployment *client.Deployment,
	timeout time.Duration,
	out *output.Writer,
) (*client.Deployment, error) {
	opts := workflow.DefaultSettleOptions()
	opts.Timeout = timeout

	started := time.Now()
	degradedSince := time.Time{}
	previous := ""

	for {
		now := time.Now()
		snap := workflow.DeploymentSnapshot{
			Phase:     deployment.Status.Phase,
			Readiness: deployment.Status.Readiness,
			Health:    deployment.Status.Health,
		}
		degradedSince = workflow.TrackDegraded(degradedSince, snap.Phase, now)

		if snap.Phase != previous {
			out.Muted("%s %s", now.UTC().Format(stampLayout), snap.Phase)

			previous = snap.Phase
		}

		if workflow.Settle(snap, started, degradedSince, now, opts).Terminal() {
			return deployment, nil
		}

		if err := sleepFor(ctx, statusPollInterval); err != nil {
			// The wait only fails when the caller interrupted; the last
			// snapshot is still the honest answer.
			return deployment, nil //nolint:nilerr // an interrupted watch is not a read failure
		}

		next, err := api.GetDeploymentByName(ctx, orgID, deployment.Metadata.Name)
		if err != nil {
			return nil, apiFailure("Could not read the deployment", err)
		}

		deployment = next
	}
}

// sleepFor waits, or returns as soon as ctx ends.
func sleepFor(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return clierrors.Errorf("wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// renderStatus writes the answer: JSON on stdout, or a human summary.
func renderStatus(
	ctx context.Context,
	api deploymentReader,
	orgID string,
	deployment *client.Deployment,
	flags *statusFlags,
	out *output.Writer,
) error {
	endpoints, _ := api.ListEndpoints(ctx, orgID, deployment.Metadata.ID) //nolint:errcheck // a missing endpoint list is not a failure of `status`
	payload := newStatusPayload(deployment, endpoints)

	if flags.conditions {
		payload.Conditions = conditionPayloads(deployment.Status.Conditions)
	}

	if flags.replicas {
		payload.Replicas = replicaPayloads(ctx, api, deployment.Metadata.ID)
	}

	if out.JSON {
		return writeJSONLine(out, payload)
	}

	renderStatusHuman(out, &payload)

	return nil
}

// newStatusPayload projects a deployment onto the JSON contract.
func newStatusPayload(deployment *client.Deployment, endpoints []client.Endpoint) statusPayload {
	payload := statusPayload{
		ID:          deployment.Metadata.ID,
		Name:        deployment.Metadata.Name,
		Phase:       deployment.Status.Phase,
		Health:      deployment.Status.Health,
		Readiness:   deployment.Status.Readiness,
		Reason:      deployment.Status.Reason,
		ConfigStale: deployment.Status.IsConfigStale,
		CreatedAt:   formatTimestamp(deployment.Metadata.CreatedAt.Time),
		UpdatedAt:   formatTimestamp(deployment.Metadata.UpdatedAt.Time),
		Conditions:  []conditionPayload{},
		Replicas:    []replicaPayload{},
		Endpoints:   endpointPayloads(endpoints),
	}

	if url := client.PublicURL(endpoints); url != "" {
		payload.URL = &url
	}

	if failure := deployment.Status.Error; failure != nil {
		payload.Error = &deployErrorPayload{
			Code:        failure.Code,
			Title:       failure.Title,
			Detail:      failure.Detail,
			Remediation: failure.Remediation,
			Origin:      failure.Origin,
			IsRetryable: failure.IsRetryable,
			CanRetry:    failure.IsRetryable && deployment.Status.AllowedActions.Allows(client.ActionRetry),
		}
	}

	return payload
}

func endpointPayloads(endpoints []client.Endpoint) []endpointPayload {
	rows := make([]endpointPayload, 0, len(endpoints))
	for index := range endpoints {
		endpoint := &endpoints[index]
		rows = append(rows, endpointPayload{
			URL:        endpoint.PublicURL,
			Protocol:   endpoint.Protocol,
			Port:       endpoint.ContainerPort,
			Visibility: endpoint.Visibility,
			State:      endpoint.State,
		})
	}

	return rows
}

func conditionPayloads(conditions []client.DeploymentCondition) []conditionPayload {
	rows := make([]conditionPayload, 0, len(conditions))
	for index := range conditions {
		condition := &conditions[index]
		rows = append(rows, conditionPayload{
			Type:    condition.Type,
			Status:  condition.Status,
			Reason:  condition.Reason,
			Message: condition.Message,
		})
	}

	return rows
}

// replicaPayloads flattens the replica groups, best effort.
func replicaPayloads(ctx context.Context, api deploymentReader, deploymentID string) []replicaPayload {
	groups, err := api.ListReplicas(ctx, deploymentID)
	if err != nil {
		return []replicaPayload{}
	}

	now := time.Now()
	rows := []replicaPayload{}

	for groupIndex := range groups {
		group := &groups[groupIndex]
		for replicaIndex := range group.Replicas {
			replica := &group.Replicas[replicaIndex]
			rows = append(rows, replicaPayload{
				Component: group.ComponentName,
				Name:      replica.Name,
				State:     replica.State,
				Health:    replica.Health,
				Restarts:  replica.Restarts,
				Age:       relativeAge(replica.StartedAt.Time, now),
			})
		}
	}

	return rows
}

// renderStatusHuman writes the summary: the URL on stdout, everything else on
// stderr, so `musher status api` still pipes cleanly.
func renderStatusHuman(out *output.Writer, payload *statusPayload) {
	out.Muted("Name:      %s", payload.Name)
	out.Muted("Phase:     %s", orDash(payload.Phase))
	out.Muted("Health:    %s", orDash(payload.Health))
	out.Muted("Readiness: %s", orDash(payload.Readiness))

	if payload.ConfigStale {
		out.Warning("The running configuration is out of date; redeploy to apply it.")
	}

	if payload.Error != nil {
		out.Failure("%s", messageOr(payload.Error.Title, "The deployment failed"))

		if payload.Error.Remediation != "" {
			out.Info("%s", payload.Error.Remediation)
		}
	}

	renderConditionTable(out, payload.Conditions)
	renderReplicaTable(out, payload.Replicas)

	if payload.URL != nil {
		fmt.Fprintln(out.Out, *payload.URL)
	}
}

func renderConditionTable(out *output.Writer, conditions []conditionPayload) {
	if len(conditions) == 0 {
		return
	}

	writer := tabwriter.NewWriter(out.Err, 0, 0, 2, ' ', 0)

	fmt.Fprintln(writer, "\nCONDITION\tSTATUS\tREASON\tMESSAGE")

	for _, condition := range conditions {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n",
			condition.Type, condition.Status, orDash(condition.Reason), orDash(condition.Message))
	}

	_ = writer.Flush() //nolint:errcheck // a failed stderr flush is not actionable
}

func renderReplicaTable(out *output.Writer, replicas []replicaPayload) {
	if len(replicas) == 0 {
		return
	}

	writer := tabwriter.NewWriter(out.Err, 0, 0, 2, ' ', 0)

	fmt.Fprintln(writer, "\nREPLICA\tCOMPONENT\tSTATE\tHEALTH\tRESTARTS\tAGE")

	for _, replica := range replicas {
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d\t%s\n",
			orDash(replica.Name), orDash(replica.Component), orDash(replica.State),
			orDash(replica.Health), replica.Restarts, replica.Age)
	}

	_ = writer.Flush() //nolint:errcheck // a failed stderr flush is not actionable
}

// resolveDeploymentName takes the name from the command line, falling back to
// the deployment file so `musher status` works from a project directory.
func resolveDeploymentName(args []string) (string, error) {
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		return strings.TrimSpace(args[0]), nil
	}

	workdir, err := os.Getwd()
	if err == nil {
		if path := deployspec.Discover(workdir); path != "" {
			if app, loadErr := deployspec.Load(path); loadErr == nil && app.Metadata.Name != "" {
				return app.Metadata.Name, nil
			}
		}
	}

	return "", &clierrors.CLIError{
		Message: "No deployment name",
		Hint:    "Pass a name, or run this from a directory containing musher.yaml",
		Code:    clierrors.ExitUsage,
	}
}
