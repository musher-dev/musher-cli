package runner

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/env"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// attemptTokenPrefix is how a run attempt token starts.
const attemptTokenPrefix = "mush_run_"

// DefaultBuildKitHost is the guest's buildkitd socket (guest protocol v2).
const DefaultBuildKitHost = "unix:///run/buildkit/buildkitd.sock"

// DefaultScratchDir is where the guest mounts its scratch disk.
const DefaultScratchDir = "/scratch"

// Inputs is the guest's inputs file, written by the host agent from the run's
// inputs: {"api_url": ..., "snapshot_id": ...} or {..., "build_id": ...}.
type Inputs struct {
	APIURL     string `json:"api_url"`
	SnapshotID string `json:"snapshot_id"`
	BuildID    string `json:"build_id"`
}

// Environment is everything runner mode reads from the guest. It never reads
// the CLI's config file, profiles or keyring.
type Environment struct {
	APIURL      string
	Token       string
	Inputs      Inputs
	ResultsFile string
	ScratchDir  string
}

// LoadEnvironment reads the guest's environment and inputs file.
func LoadEnvironment() (*Environment, error) {
	environ := &Environment{
		APIURL:      strings.TrimSpace(env.Get(env.APIURL)),
		Token:       strings.TrimSpace(env.Get(env.APIKey)),
		ResultsFile: strings.TrimSpace(env.Get(env.ResultsFile)),
		ScratchDir:  strings.TrimSpace(env.Get(env.TmpDir)),
	}

	if path := strings.TrimSpace(env.Get(env.InputsFile)); path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // G304: the host agent names its own inputs file
		if err != nil {
			return nil, clierrors.Wrap(clierrors.ExitConfig, "Cannot read the inputs file "+path, err)
		}

		if err := json.Unmarshal(data, &environ.Inputs); err != nil {
			return nil, clierrors.Wrap(clierrors.ExitConfig, "The inputs file "+path+" is not a JSON object", err)
		}
	}

	if environ.APIURL == "" {
		environ.APIURL = strings.TrimSpace(environ.Inputs.APIURL)
	}

	if environ.ScratchDir == "" {
		environ.ScratchDir = DefaultScratchDir
	}

	return environ, nil
}

// RequireAPI checks the API URL and the attempt token are present.
func (e *Environment) RequireAPI() error {
	if e.APIURL == "" {
		return &clierrors.CLIError{
			Message: "No API URL in runner mode",
			Hint:    "Set MUSHER_API_URL, or api_url in the MUSHER_INPUTS_FILE inputs file",
			Code:    clierrors.ExitConfig,
		}
	}

	if !strings.HasPrefix(e.Token, attemptTokenPrefix) {
		return &clierrors.CLIError{
			Message: "MUSHER_API_KEY does not hold a run attempt token",
			Hint:    "Runner mode authenticates only with the attempt's mush_run_ token, which the host agent sets",
			Code:    clierrors.ExitConfig,
		}
	}

	return nil
}

// ResolveID returns the id the flag names, else the one the inputs file
// names, and refuses the two disagreeing.
func ResolveID(flag, fromInputs, what string) (uuid.UUID, error) {
	flag = strings.TrimSpace(flag)
	fromInputs = strings.TrimSpace(fromInputs)

	raw := flag
	if raw == "" {
		raw = fromInputs
	}

	if flag != "" && fromInputs != "" && !strings.EqualFold(flag, fromInputs) {
		return uuid.Nil, &clierrors.CLIError{
			Message: "The " + what + " flag (" + flag + ") and the inputs file (" + fromInputs + ") disagree",
			Code:    clierrors.ExitUsage,
		}
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, &clierrors.CLIError{
			Message: "A " + what + " id is required: a UUID",
			Hint:    "Pass --" + what + " <id>, or set " + what + "_id in the inputs file",
			Code:    clierrors.ExitUsage,
		}
	}

	return id, nil
}

// TokenExpiry reads the expiry a run attempt token carries
// (mush_run_<kid>.<attempt>.<fencing>.<expiry epoch seconds>.<signature>),
// so the runner never outlives its token. ok is false for a token it cannot
// read; the API stays the authority either way.
func TokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(strings.TrimPrefix(token, attemptTokenPrefix), ".")
	if len(parts) != 5 {
		return time.Time{}, false
	}

	seconds, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}

	return time.Unix(seconds, 0), true
}
