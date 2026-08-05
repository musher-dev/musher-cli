package main

import (
	"context"
	"maps"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/output"
	"github.com/musher-dev/musher-cli/internal/prompt"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/musher-dev/musher-cli/internal/workflow"
)

// exitInterrupted is the conventional shell code for a process killed by
// SIGINT (128 + 2). It is not in internal/errors because it is not a Musher
// exit code: it is the shell's.
const exitInterrupted = 130

// doubleInterruptWindow is how quickly a second Ctrl-C means "stop pretending
// to be graceful".
const doubleInterruptWindow = 2 * time.Second

// waitTargets maps the --wait-for vocabulary onto the workflow's target.
var waitTargets = map[string]workflow.WaitTarget{
	"ready": workflow.WaitReady,
	"url":   workflow.WaitURL,
}

// deployFlags holds the parsed command line for `musher deploy`.
type deployFlags struct {
	image         string
	name          string
	file          string
	environment   string
	kind          string
	size          string
	waitFor       string
	envPairs      []string
	envFile       string
	port          int
	replicas      int
	detach        bool
	watch         bool
	yes           bool
	timeout       time.Duration
	degradedGrace time.Duration
}

func newDeployCmd() *cobra.Command {
	flags := &deployFlags{}

	cmd := &cobra.Command{
		Use:   "deploy [name]",
		Short: "Deploy a container image",
		Long: `Deploy a container image to the Musher platform.

With no --image, the deployment is read from ./musher.yaml. Flags always win
over the file, so a pinned image can be overridden for one run without editing
anything.

The deployment name is the natural key: deploying the same name again updates
the existing deployment in place, keeping its id, its timeline, and its URL.`,
		Example: `  # Deploy an image directly
  musher deploy api --image ghcr.io/acme/api:v1.4.2 --port 8080

  # Deploy from ./musher.yaml
  musher deploy

  # Submit and return immediately
  musher deploy api --image ghcr.io/acme/api:v1.4.2 --detach

  # Machine-readable, one object on stdout
  musher deploy --json | jq -r '.url'`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeploy(cmd, args, flags)
		},
	}

	registerDeployFlags(cmd, flags)

	return cmd
}

func registerDeployFlags(cmd *cobra.Command, flags *deployFlags) {
	set := cmd.Flags()

	set.StringVar(&flags.image, "image", "", "Container image to run (must be a pinned tag or digest)")
	set.StringVar(&flags.name, "name", "", "Deployment name (defaults to the positional argument or the file)")
	set.StringVarP(&flags.file, "file", "f", "", "Deployment file to read (default: ./musher.yaml)")
	set.StringVar(&flags.environment, "environment", "", "Environment to deploy into (default: the default STANDARD one)")
	set.StringVar(&flags.kind, "kind", "", "Workload kind: SERVICE, WORKER, JOB, CRON")
	set.StringVar(&flags.size, "size", "", "Compute profile for the workload")
	set.IntVar(&flags.port, "port", 0, "Container port to expose publicly")
	set.IntVar(&flags.replicas, "replicas", 0, "Number of replicas to run")
	set.StringArrayVarP(&flags.envPairs, "env", "e", nil, "Environment variable as KEY=VALUE (repeatable)")
	set.StringVar(&flags.envFile, "env-file", "", "File of KEY=VALUE environment variables")
	set.BoolVar(&flags.detach, "detach", false, "Submit the deployment and return immediately")
	set.BoolVar(&flags.watch, "watch", true, "Follow the deployment until it settles")
	set.DurationVar(&flags.timeout, "timeout", 0, "How long to watch before giving up (default: 15m)")
	set.DurationVar(&flags.degradedGrace, "degraded-grace", 0,
		"How long DEGRADED is tolerated before it is a failure (default: 1m)")
	set.StringVar(&flags.waitFor, "wait-for", "ready", "Success condition: ready or url")
	set.BoolVarP(&flags.yes, "yes", "y", false, "Do not ask for confirmation")
}

// runDeploy is the whole command: resolve intent, confirm, run, report.
func runDeploy(cmd *cobra.Command, args []string, flags *deployFlags) error {
	out := output.FromContext(cmd.Context())
	cfg := config.FromContext(cmd.Context())

	input, err := resolveDeployInput(args, flags, cfg)
	if err != nil {
		return err
	}

	opts, err := deployOptions(cmd, flags, cfg)
	if err != nil {
		return err
	}

	if confirmErr := confirmDeploy(out, input, flags.yes); confirmErr != nil {
		return confirmErr
	}

	api, err := requireAuthFromContext(cmd.Context())
	if err != nil {
		return err
	}

	return executeDeploy(cmd.Context(), out, api, input, opts)
}

// executeDeploy runs the workflow under an interrupt guard and reports.
func executeDeploy(
	ctx context.Context,
	out *output.Writer,
	api workflow.DeployAPI,
	input *workflow.DeployInput,
	opts workflow.DeployOptions,
) error {
	reporter := newDeployReporter(out)

	watchCtx, guard, stop := watchWithInterrupt(ctx, out, input.Name)
	defer stop()

	result, err := workflow.Deploy(watchCtx, api, reporter, *input, opts)

	if guard.interrupted() && result != nil {
		// A caller who pressed Ctrl-C asked to stop watching, not to stop
		// deploying. That is a detach, and a detach is a success.
		result.Detached = true

		return writeDeployAnswer(out, result)
	}

	if guard.aborted() {
		return &clierrors.CLIError{
			Message: "Interrupted",
			Code:    exitInterrupted,
		}
	}

	if result != nil {
		if writeErr := writeDeployAnswer(out, result); writeErr != nil {
			return writeErr
		}
	}

	if err != nil {
		return deployFailure(err, input.Name)
	}

	return nil
}

// resolveDeployInput merges the deployment file, the flags, and configuration
// into one request. Flags always win: overriding a pinned image for a single
// run must not require editing a file that is under review.
func resolveDeployInput(args []string, flags *deployFlags, cfg *config.Config) (*workflow.DeployInput, error) {
	input := &workflow.DeployInput{
		Kind:         deployspec.KindService,
		Replicas:     1,
		Environment:  cfg.Environment(),
		Organization: cfg.Organization(),
		Size:         cfg.DeploySize(),
	}

	app, err := loadDeploySpec(flags)
	if err != nil {
		return nil, err
	}

	if app != nil {
		applyAppSpec(input, app)
	}

	applyDeployFlags(input, flags)

	if len(args) == 1 {
		input.Name = args[0]
	}

	if input.Name == "" {
		return nil, &clierrors.CLIError{
			Message: "No deployment name",
			Hint:    "Pass a name (musher deploy NAME --image ...) or set metadata.name in musher.yaml",
			Code:    clierrors.ExitUsage,
		}
	}

	env, err := collectEnv(flags)
	if err != nil {
		return nil, err
	}

	if len(env) > 0 {
		if input.Env == nil {
			input.Env = make(map[string]string, len(env))
		}

		maps.Copy(input.Env, env)
	}

	return input, nil
}

// loadDeploySpec reads the deployment file, if there is one to read.
func loadDeploySpec(flags *deployFlags) (*deployspec.App, error) {
	path := strings.TrimSpace(flags.file)
	if path == "" {
		if strings.TrimSpace(flags.image) != "" {
			// An explicit image is a complete request; do not surprise the
			// caller by silently merging a file they did not mention.
			return nil, nil //nolint:nilnil // "no file involved" is a valid answer
		}

		workdir, err := os.Getwd()
		if err != nil {
			return nil, clierrors.Wrap(clierrors.ExitGeneral, "Cannot determine the working directory", err)
		}

		path = deployspec.Discover(workdir)
	}

	if path == "" {
		return nil, &clierrors.CLIError{
			Message: "Nothing to deploy",
			Hint:    "Pass --image, or run this from a directory containing musher.yaml",
			Code:    clierrors.ExitUsage,
		}
	}

	app, err := deployspec.Load(path)
	if err != nil {
		return nil, clierrors.Wrap(clierrors.ExitInvalidSpec, "Invalid deployment file", err).
			WithHint("Fix " + path + " and run the command again")
	}

	return app, nil
}

// applyAppSpec copies the file's intent onto the request.
func applyAppSpec(input *workflow.DeployInput, app *deployspec.App) {
	input.Name = app.Metadata.Name
	input.Image = app.Spec.Workload.Image
	input.Kind = app.Spec.Workload.Kind

	if app.Spec.Environment != "" {
		input.Environment = app.Spec.Environment
	}

	if app.Spec.Size != "" {
		input.Size = app.Spec.Size
	}

	if app.Spec.Replicas > 0 {
		input.Replicas = app.Spec.Replicas
	}

	for _, endpoint := range app.Spec.Workload.Endpoints {
		if endpoint.ContainerPort > 0 {
			input.Port = endpoint.ContainerPort

			break
		}
	}

	if len(app.Spec.Workload.Env) > 0 {
		input.Env = maps.Clone(app.Spec.Workload.Env)
	}
}

// applyDeployFlags overlays the command line onto the request.
func applyDeployFlags(input *workflow.DeployInput, flags *deployFlags) {
	overlay := []struct {
		value  string
		target *string
	}{
		{flags.image, &input.Image},
		{flags.name, &input.Name},
		{flags.environment, &input.Environment},
		{flags.size, &input.Size},
		{strings.ToUpper(strings.TrimSpace(flags.kind)), &input.Kind},
	}

	for _, field := range overlay {
		if trimmed := strings.TrimSpace(field.value); trimmed != "" {
			*field.target = trimmed
		}
	}

	if flags.port > 0 {
		input.Port = flags.port
	}

	if flags.replicas > 0 {
		input.Replicas = flags.replicas
	}
}

// deployOptions turns the watch flags and config into workflow options.
func deployOptions(cmd *cobra.Command, flags *deployFlags, cfg *config.Config) (workflow.DeployOptions, error) {
	target, ok := waitTargets[strings.ToLower(strings.TrimSpace(flags.waitFor))]
	if !ok {
		return workflow.DeployOptions{}, &clierrors.CLIError{
			Message: "Unknown --wait-for value " + flags.waitFor,
			Hint:    "Use --wait-for ready or --wait-for url",
			Code:    clierrors.ExitUsage,
		}
	}

	watch := flags.watch
	if !cmd.Flags().Changed("watch") {
		watch = cfg.DeployWait()
	}

	timeout := flags.timeout
	if timeout <= 0 {
		timeout = cfg.DeployTimeout()
	}

	return workflow.DeployOptions{
		Detach:        flags.detach,
		Watch:         watch && !flags.detach,
		Timeout:       timeout,
		DegradedGrace: flags.degradedGrace,
		WaitFor:       target,
	}, nil
}

// collectEnv merges --env-file and --env into one map, with --env winning.
func collectEnv(flags *deployFlags) (map[string]string, error) {
	env := map[string]string{}

	if path := strings.TrimSpace(flags.envFile); path != "" {
		data, err := safeio.ReadFile(path)
		if err != nil {
			return nil, clierrors.Wrap(clierrors.ExitConfig, "Cannot read the env file", err)
		}

		if err := parseEnvLines(string(data), env); err != nil {
			return nil, err
		}
	}

	for _, pair := range flags.envPairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(key) == "" {
			return nil, &clierrors.CLIError{
				Message: "Invalid --env value " + pair,
				Hint:    "Use --env KEY=VALUE",
				Code:    clierrors.ExitUsage,
			}
		}

		env[strings.TrimSpace(key)] = value
	}

	return env, nil
}

// parseEnvLines reads a KEY=VALUE file, ignoring blanks and # comments.
func parseEnvLines(content string, env map[string]string) error {
	for index, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, value, found := strings.Cut(trimmed, "=")
		if !found || strings.TrimSpace(key) == "" {
			return &clierrors.CLIError{
				Message: "Invalid env file entry on line " + strconv.Itoa(index+1),
				Hint:    "Each line must be KEY=VALUE, blank, or a # comment",
				Code:    clierrors.ExitConfig,
			}
		}

		env[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}

	return nil
}

// confirmDeploy shows the plan and asks before doing anything irreversible.
func confirmDeploy(out *output.Writer, input *workflow.DeployInput, yes bool) error {
	prompter := prompt.New(out)
	if yes || out.NoInput || out.JSON || out.Quiet || !prompter.CanPrompt() {
		return nil
	}

	out.Muted("Deploy %s", input.Name)
	out.Muted("  image:       %s", input.Image)
	out.Muted("  environment: %s", environmentLabel(input.Environment))
	out.Muted("  replicas:    %d", input.Replicas)

	confirmed, err := prompter.Confirm("Continue?", true)
	if err != nil {
		return clierrors.Wrap(clierrors.ExitGeneral, "Could not read the confirmation", err)
	}

	if !confirmed {
		return &clierrors.CLIError{
			Message: "Deploy canceled",
			Code:    clierrors.ExitSuccess,
		}
	}

	return nil
}

// environmentLabel renders an unset environment as the implied default.
func environmentLabel(name string) string {
	if strings.TrimSpace(name) == "" {
		return "(default)"
	}

	return name
}

// deployInterrupt turns Ctrl-C into a detach instead of a cancellation.
//
// A deployment is server-side work: canceling the CLI does not and must not
// cancel it. The first interrupt therefore stops watching and says so; only a
// second, deliberate one within a couple of seconds gives up on the process
// itself.
type deployInterrupt struct {
	mu    sync.Mutex
	first time.Time
	hard  bool
}

// record notes an interrupt and reports whether it was the deliberate second.
func (d *deployInterrupt) record(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.first.IsZero() && now.Sub(d.first) <= doubleInterruptWindow {
		d.hard = true

		return true
	}

	d.first = now

	return false
}

// interrupted reports whether the watch was detached by a first interrupt.
func (d *deployInterrupt) interrupted() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return !d.first.IsZero() && !d.hard
}

// aborted reports whether a second interrupt asked the process to give up.
func (d *deployInterrupt) aborted() bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.hard
}

// watchWithInterrupt derives a watch context that a SIGINT detaches from.
func watchWithInterrupt(
	ctx context.Context,
	out *output.Writer,
	name string,
) (context.Context, *deployInterrupt, func()) {
	guard := &deployInterrupt{}
	watchCtx, cancel := context.WithCancel(ctx)

	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt)

	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-done:
				return
			case <-signals:
				if guard.record(time.Now()) {
					// Hand SIGINT back to the runtime so a third one always
					// terminates, even if the watch is wedged.
					signal.Stop(signals)
					cancel()

					return
				}

				renderDetachNotice(out, name)
				cancel()
			}
		}
	}()

	var once sync.Once

	return watchCtx, guard, func() {
		once.Do(func() {
			signal.Stop(signals)
			close(done)
			cancel()
		})
	}
}

// renderDetachNotice explains, on stderr, that the deployment outlives the CLI.
func renderDetachNotice(out *output.Writer, name string) {
	out.Errorln()
	out.Warning("Stopped watching. The deployment is still running.")
	out.Muted("  Follow it:  musher status %s --watch", name)
	out.Muted("  Read logs:  musher logs %s --follow", name)
}
