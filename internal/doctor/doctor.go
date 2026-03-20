// Package doctor provides diagnostic checks for Musher CLI health.
//
// This package implements a check framework that validates:
//   - Local directory structure and permissions
//   - Configuration file validity
//   - Credential file security
//   - API connectivity and response time
//   - Authentication status and credential source
//   - CLI version against latest release
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/musher-dev/musher-cli/internal/auth"
	"github.com/musher-dev/musher-cli/internal/buildinfo"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/config"
	"github.com/musher-dev/musher-cli/internal/paths"
	"github.com/musher-dev/musher-cli/internal/safeio"
	"github.com/musher-dev/musher-cli/internal/update"
)

// Status represents the result of a diagnostic check.
type Status int

const (
	// StatusPass indicates the check passed.
	StatusPass Status = iota
	// StatusWarn indicates a non-critical issue.
	StatusWarn
	// StatusFail indicates a critical failure.
	StatusFail
)

// Result holds the outcome of a single check.
type Result struct {
	Name    string
	Status  Status
	Message string
	Detail  string // Optional additional detail
}

// Check is a diagnostic check function.
type Check func(ctx context.Context) Result

// Runner executes diagnostic checks.
type Runner struct {
	checks []namedCheck
}

type namedCheck struct {
	name  string
	check Check
}

// New creates a new diagnostic runner.
func New() *Runner {
	r := &Runner{}

	// Register default checks — prerequisites first
	r.AddCheck("Directory Structure", checkDirectoryStructure)
	r.AddCheck("Config File", checkConfigFile)
	r.AddCheck("Credentials File", checkCredentialsFile)
	r.AddCheck("Proxy Environment", checkProxyEnvironment)
	r.AddCheck("Custom CA Bundle", checkCustomCABundle)
	r.AddCheck("API Connectivity", checkAPIConnectivity)
	r.AddCheck("Clock Skew", checkClockSkew)
	r.AddCheck("Authentication", checkAuthentication)
	r.AddCheck("CLI Version", checkCLIVersion)

	return r
}

// AddCheck registers a diagnostic check.
func (r *Runner) AddCheck(name string, check Check) {
	r.checks = append(r.checks, namedCheck{name: name, check: check})
}

// Run executes all registered checks and returns the results.
func (r *Runner) Run(ctx context.Context) []Result {
	results := make([]Result, 0, len(r.checks))

	for _, nc := range r.checks {
		result := nc.check(ctx)
		result.Name = nc.name
		results = append(results, result)
	}

	return results
}

// Summary returns counts of passed, failed, and warning checks.
func Summary(results []Result) (passed, failed, warnings int) {
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			passed++
		case StatusFail:
			failed++
		case StatusWarn:
			warnings++
		}
	}

	return passed, failed, warnings
}

// checkDirectoryStructure verifies XDG roots resolve and are accessible.
func checkDirectoryStructure(context.Context) Result {
	type root struct {
		name string
		fn   func() (string, error)
	}

	roots := []root{
		{"config", paths.ConfigRoot},
		{"state", paths.StateRoot},
		{"cache", paths.CacheRoot},
	}

	var missing []string

	for _, r := range roots {
		dir, err := r.fn()
		if err != nil {
			return Result{
				Status:  StatusFail,
				Message: "Cannot resolve directories",
				Detail:  "$HOME must be set",
			}
		}

		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, r.name)
				continue
			}

			return Result{
				Status:  StatusFail,
				Message: fmt.Sprintf("Cannot access %s directory", r.name),
				Detail:  err.Error(),
			}
		}

		if !info.IsDir() {
			return Result{
				Status:  StatusFail,
				Message: fmt.Sprintf("%s path is not a directory: %s", r.name, dir),
				Detail:  "Remove the file and let musher recreate it",
			}
		}

		// Check writable by attempting to create a unique temp file
		f, err := os.CreateTemp(dir, ".musher-doctor-probe.*")
		if err != nil {
			return Result{
				Status:  StatusFail,
				Message: fmt.Sprintf("%s directory not writable: %s", r.name, dir),
				Detail:  "Check directory permissions",
			}
		}

		_ = f.Close()
		_ = os.Remove(f.Name())
	}

	if len(missing) > 0 {
		return Result{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Missing directories: %s", strings.Join(missing, ", ")),
			Detail:  "Created on first use by any musher command",
		}
	}

	return Result{
		Status:  StatusPass,
		Message: "Config, state, and cache directories OK",
	}
}

// checkConfigFile validates YAML syntax of the config file if present.
func checkConfigFile(context.Context) Result {
	configDir, err := paths.ConfigRoot()
	if err != nil {
		return Result{
			Status:  StatusPass,
			Message: "No config file (using defaults)",
		}
	}

	configPath := filepath.Join(configDir, "config.yaml")

	data, err := safeio.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{
				Status:  StatusPass,
				Message: "No config file (using defaults)",
			}
		}

		return Result{
			Status:  StatusFail,
			Message: "Cannot read config file",
			Detail:  err.Error(),
		}
	}

	var parsed any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return Result{
			Status:  StatusFail,
			Message: "Invalid YAML in config file",
			Detail:  fmt.Sprintf("%s — fix or delete %s", err.Error(), configPath),
		}
	}

	return Result{
		Status:  StatusPass,
		Message: configPath,
	}
}

// checkCredentialsFile checks permissions on the credentials fallback file.
func checkCredentialsFile(context.Context) Result {
	credPath, err := paths.CredentialsFile()
	if err != nil {
		return Result{
			Status:  StatusPass,
			Message: "Not present (using keyring or env)",
		}
	}

	info, err := os.Stat(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{
				Status:  StatusPass,
				Message: "Not present (using keyring or env)",
			}
		}

		return Result{
			Status:  StatusFail,
			Message: "Cannot access credentials file",
			Detail:  err.Error(),
		}
	}

	// Skip permission check on non-Unix platforms
	if runtime.GOOS == "windows" {
		return Result{
			Status:  StatusPass,
			Message: credPath,
		}
	}

	mode := info.Mode().Perm()
	if mode&(fs.FileMode(0o077)) != 0 {
		return Result{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Credentials file too permissive (%04o)", mode),
			Detail:  fmt.Sprintf("chmod 600 %s", credPath),
		}
	}

	return Result{
		Status:  StatusPass,
		Message: credPath,
	}
}

// checkAPIConnectivity tests connection to the API endpoint.
func checkAPIConnectivity(ctx context.Context) Result {
	cfg := config.Load()
	apiURL := cfg.APIURL()

	probe := client.ProbeHealth(ctx, apiURL, cfg.CACertFile())
	if probe.Reachable {
		return Result{
			Status:  StatusPass,
			Message: fmt.Sprintf("%s (%dms)", apiURL, probe.Latency.Milliseconds()),
		}
	}

	return Result{
		Status:  StatusFail,
		Message: apiURL,
		Detail:  probe.Error,
	}
}

// checkAuthentication validates stored credentials.
func checkAuthentication(ctx context.Context) Result {
	source, apiKey := auth.GetCredentials()

	if apiKey == "" {
		return Result{
			Status:  StatusFail,
			Message: "Not authenticated",
			Detail:  "Run 'musher login' to authenticate",
		}
	}

	// Validate the key
	cfg := config.Load()

	httpClient, clientErr := client.NewInstrumentedHTTPClient(cfg.CACertFile())
	if clientErr != nil {
		return Result{
			Status:  StatusFail,
			Message: "HTTP client setup failed",
			Detail:  clientErr.Error(),
		}
	}

	c := client.NewWithHTTPClient(cfg.APIURL(), apiKey, httpClient)

	identity, err := c.ValidateKey(ctx)
	if err != nil {
		return Result{
			Status:  StatusFail,
			Message: fmt.Sprintf("Invalid credentials (via %s)", source),
			Detail:  err.Error(),
		}
	}

	return Result{
		Status:  StatusPass,
		Message: fmt.Sprintf("%s (via %s)", identity.CredentialName, source),
	}
}

func checkClockSkew(ctx context.Context) Result {
	cfg := config.Load()

	probe := client.ProbeHealth(ctx, cfg.APIURL(), cfg.CACertFile())
	if !probe.Reachable {
		return Result{
			Status:  StatusWarn,
			Message: "Clock skew check skipped",
			Detail:  "API not reachable",
		}
	}

	if probe.ServerTime == nil {
		return Result{
			Status:  StatusWarn,
			Message: "Clock skew unknown",
			Detail:  "API response did not include a Date header",
		}
	}

	skew := time.Since(*probe.ServerTime)
	if skew < 0 {
		skew = -skew
	}

	if skew > 2*time.Minute {
		return Result{
			Status:  StatusWarn,
			Message: fmt.Sprintf("Clock skew detected (%s)", skew.Round(time.Second)),
			Detail:  "Sync your system clock (NTP) to avoid auth token validity issues",
		}
	}

	return Result{
		Status:  StatusPass,
		Message: fmt.Sprintf("Within tolerance (%s)", skew.Round(time.Second)),
	}
}

func checkProxyEnvironment(context.Context) Result {
	keys := []string{
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"NO_PROXY", "no_proxy",
	}

	var active []string

	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			active = append(active, key)
		}
	}

	if len(active) == 0 {
		return Result{
			Status:  StatusPass,
			Message: "No proxy environment variables detected",
		}
	}

	return Result{
		Status:  StatusWarn,
		Message: fmt.Sprintf("Proxy variables detected: %s", strings.Join(active, ", ")),
		Detail:  "If requests fail with TLS errors, configure MUSHER_NETWORK_CA_CERT_FILE with your corporate proxy CA bundle",
	}
}

func checkCustomCABundle(context.Context) Result {
	cfg := config.Load()

	caPath := strings.TrimSpace(cfg.CACertFile())
	if caPath == "" {
		return Result{
			Status:  StatusPass,
			Message: "Not configured",
		}
	}

	info, err := os.Stat(caPath)
	if err != nil {
		return Result{
			Status:  StatusFail,
			Message: "Configured file not readable",
			Detail:  err.Error(),
		}
	}

	if info.IsDir() {
		return Result{
			Status:  StatusFail,
			Message: "Configured path is a directory",
			Detail:  filepath.Clean(caPath),
		}
	}

	return Result{
		Status:  StatusPass,
		Message: filepath.Clean(caPath),
	}
}

// checkCLIVersion checks the CLI version against the latest release.
func checkCLIVersion(ctx context.Context) Result {
	current := buildinfo.Version

	if current == "dev" {
		return Result{
			Status:  StatusWarn,
			Message: "Development build (version check skipped)",
		}
	}

	if update.IsDisabled() {
		return Result{
			Status:  StatusPass,
			Message: fmt.Sprintf("v%s (update checks disabled)", current),
		}
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updater, err := update.NewUpdater()
	if err != nil {
		return Result{
			Status:  StatusWarn,
			Message: fmt.Sprintf("v%s (could not check for updates)", current),
			Detail:  err.Error(),
		}
	}

	info, err := updater.CheckLatest(checkCtx, current)
	if err != nil {
		return Result{
			Status:  StatusWarn,
			Message: fmt.Sprintf("v%s (could not check for updates)", current),
			Detail:  err.Error(),
		}
	}

	if info.UpdateAvailable {
		return Result{
			Status:  StatusWarn,
			Message: fmt.Sprintf("v%s (v%s available)", current, info.LatestVersion),
			Detail:  "Run 'musher update' to update",
		}
	}

	return Result{
		Status:  StatusPass,
		Message: fmt.Sprintf("v%s (latest)", current),
	}
}

// RenderResults formats diagnostic results to the given output writer.
func RenderResults(results []Result, printFn, successFn, warningFn, failureFn, mutedFn func(format string, args ...any)) {
	maxNameLen := 0
	for _, r := range results {
		if len(r.Name) > maxNameLen {
			maxNameLen = len(r.Name)
		}
	}

	for _, r := range results {
		symbol := r.Status.Symbol()
		padding := maxNameLen - len(r.Name) + 4

		switch r.Status {
		case StatusPass:
			successFn("%-*s%s", len(r.Name)+padding, r.Name, r.Message)
		case StatusWarn:
			warningFn("%-*s%s", len(r.Name)+padding, r.Name, r.Message)
		case StatusFail:
			failureFn("%-*s%s", len(r.Name)+padding, r.Name, r.Message)
		default:
			printFn("%s %-*s%s\n", symbol, len(r.Name)+padding, r.Name, r.Message)
		}

		if r.Detail != "" {
			mutedFn("    %s", r.Detail)
		}
	}
}

// Symbol returns the status symbol for display.
func (s Status) Symbol() string {
	switch s {
	case StatusPass:
		return checkMark
	case StatusWarn:
		return warningMark
	case StatusFail:
		return xMark
	default:
		return "?"
	}
}

const (
	checkMark   = "\u2713" // check
	xMark       = "\u2717" // x
	warningMark = "\u26A0" // warning
)
