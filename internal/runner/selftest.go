package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/musher-dev/musher-cli/internal/build"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// SelfTestSchemaVersion is the version of the self-test results document,
// contracts/runner/self-test.schema.json. It changes only when a key is
// removed or changes meaning; added checks keep it.
const SelfTestSchemaVersion = 1

// Check statuses.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// Check names, the keys of SelfTestResults.Checks. Stable: the host agent
// maps each to an isolation condition.
const (
	CheckBuildKitWorkers  = "buildkit_workers"
	CheckBuildKitScratch  = "buildkit_scratch_build"
	CheckAPIReachable     = "api_reachable"
	CheckRegistryReach    = "registry_reachable"
	CheckDeniedMetadata   = "egress_denied_metadata"
	CheckDeniedPrivate    = "egress_denied_rfc1918"
	CheckDeniedCGNAT      = "egress_denied_cgnat"
	CheckScratchWritable  = "scratch_writable"
	defaultDenyTimeout    = 3 * time.Second
	defaultReachTimeout   = 10 * time.Second
	defaultBuildKitBudget = 60 * time.Second
)

// SelfTestCheck is one check's verdict.
type SelfTestCheck struct {
	Status     string `json:"status"`
	Target     string `json:"target,omitempty"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// SelfTestResults is the document `musher runner self-test` writes.
type SelfTestResults struct {
	SchemaVersion int                      `json:"schema_version"`
	RunnerVersion string                   `json:"runner_version"`
	StartedAt     time.Time                `json:"started_at"`
	FinishedAt    time.Time                `json:"finished_at"`
	Passed        bool                     `json:"passed"`
	Checks        map[string]SelfTestCheck `json:"checks"`
}

// SelfTest is the guest canary's in-guest half.
type SelfTest struct {
	Version      string
	BuildKitHost string
	APIURL       string
	// RegistryHost is host[:port]; empty skips the registry check.
	RegistryHost string
	ScratchDir   string
	// Deny targets (host:port) that must not be reachable.
	DenyMetadata string
	DenyPrivate  string
	DenyCGNAT    string
	DenyTimeout  time.Duration
	// Dial and TLS seams for tests.
	Dial      func(ctx context.Context, network, address string) (net.Conn, error)
	TLSConfig *tls.Config
	// BuildKit seams for tests.
	Workers      func(ctx context.Context, address string) ([]string, error)
	ScratchBuild func(ctx context.Context, address, dir string) error
}

// Default deny-probe targets: the cloud metadata endpoint, one RFC 1918 and
// one RFC 6598 (CGNAT, 100.64.0.0/10) address.
const (
	DefaultDenyMetadata = "169.254.169.254:80"
	DefaultDenyPrivate  = "10.0.0.1:443"
	DefaultDenyCGNAT    = "100.64.0.1:443"
)

// Run runs every check, concurrently, and returns the results.
func (s *SelfTest) Run(ctx context.Context) *SelfTestResults {
	s.defaults()

	res := &SelfTestResults{
		SchemaVersion: SelfTestSchemaVersion,
		RunnerVersion: s.Version,
		StartedAt:     time.Now().UTC(),
		Checks:        map[string]SelfTestCheck{},
	}

	checks := map[string]func(ctx context.Context) SelfTestCheck{
		CheckBuildKitWorkers: s.checkWorkers,
		CheckBuildKitScratch: s.checkScratchBuild,
		CheckAPIReachable:    func(ctx context.Context) SelfTestCheck { return s.checkReach(ctx, apiTarget(s.APIURL)) },
		CheckRegistryReach:   func(ctx context.Context) SelfTestCheck { return s.checkReach(ctx, withPort(s.RegistryHost)) },
		CheckDeniedMetadata:  func(ctx context.Context) SelfTestCheck { return s.checkDenied(ctx, s.DenyMetadata) },
		CheckDeniedPrivate:   func(ctx context.Context) SelfTestCheck { return s.checkDenied(ctx, s.DenyPrivate) },
		CheckDeniedCGNAT:     func(ctx context.Context) SelfTestCheck { return s.checkDenied(ctx, s.DenyCGNAT) },
		CheckScratchWritable: s.checkScratch,
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for name, check := range checks {
		wg.Go(func() {
			start := time.Now()
			verdict := check(ctx)
			verdict.DurationMS = time.Since(start).Milliseconds()

			mu.Lock()
			res.Checks[name] = verdict
			mu.Unlock()
		})
	}

	wg.Wait()

	res.FinishedAt = time.Now().UTC()
	res.Passed = true

	for _, c := range res.Checks {
		if c.Status == StatusFail {
			res.Passed = false
		}
	}

	return res
}

func (s *SelfTest) defaults() {
	if s.Dial == nil {
		dialer := &net.Dialer{}
		s.Dial = dialer.DialContext
	}

	if s.Workers == nil {
		s.Workers = build.ProbeWorkers
	}

	if s.ScratchBuild == nil {
		s.ScratchBuild = build.ScratchBuild
	}

	if s.DenyTimeout == 0 {
		s.DenyTimeout = defaultDenyTimeout
	}
}

func (s *SelfTest) checkWorkers(ctx context.Context) SelfTestCheck {
	ctx, cancel := context.WithTimeout(ctx, defaultBuildKitBudget)
	defer cancel()

	workers, err := s.Workers(ctx, s.BuildKitHost)
	if err != nil {
		return fail(s.BuildKitHost, err.Error())
	}

	if len(workers) == 0 {
		return fail(s.BuildKitHost, "buildkitd has no workers")
	}

	return pass(s.BuildKitHost, strings.Join(workers, "; "))
}

func (s *SelfTest) checkScratchBuild(ctx context.Context) SelfTestCheck {
	ctx, cancel := context.WithTimeout(ctx, defaultBuildKitBudget)
	defer cancel()

	dir, err := os.MkdirTemp(s.ScratchDir, "self-test-build-")
	if err != nil {
		return fail(s.BuildKitHost, "create the build context: "+err.Error())
	}

	defer removeQuietly(dir)

	if err := s.ScratchBuild(ctx, s.BuildKitHost, dir); err != nil {
		return fail(s.BuildKitHost, err.Error())
	}

	return pass(s.BuildKitHost, "FROM scratch built, nothing pushed")
}

// checkReach proves TCP and a verified TLS handshake to target.
func (s *SelfTest) checkReach(ctx context.Context, target string) SelfTestCheck {
	if target == "" {
		return SelfTestCheck{Status: StatusSkip, Detail: "no target configured"}
	}

	ctx, cancel := context.WithTimeout(ctx, defaultReachTimeout)
	defer cancel()

	conn, err := s.Dial(ctx, "tcp", target)
	if err != nil {
		return fail(target, "TCP: "+err.Error())
	}

	defer closeQuietly(conn)

	host, _, splitErr := net.SplitHostPort(target)
	if splitErr != nil {
		host = target
	}

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if s.TLSConfig != nil {
		cfg = s.TLSConfig.Clone()
	}

	cfg.ServerName = host

	client := tls.Client(conn, cfg)
	if err := client.HandshakeContext(ctx); err != nil {
		return fail(target, "TLS: "+err.Error())
	}

	state := client.ConnectionState()

	return pass(target, tls.VersionName(state.Version)+" handshake verified")
}

// checkDenied passes when target cannot be connected to within the timeout:
// a reject (refused, unreachable) or a silent drop (timeout) both count.
func (s *SelfTest) checkDenied(ctx context.Context, target string) SelfTestCheck {
	if target == "" {
		return SelfTestCheck{Status: StatusSkip, Detail: "no target configured"}
	}

	ctx, cancel := context.WithTimeout(ctx, s.DenyTimeout)
	defer cancel()

	conn, err := s.Dial(ctx, "tcp", target)
	if err == nil {
		closeQuietly(conn)

		return fail(target, "connected: egress to this address is not denied")
	}

	return pass(target, "denied: "+denialKind(err))
}

func (s *SelfTest) checkScratch(_ context.Context) SelfTestCheck {
	probe := filepath.Join(s.ScratchDir, ".musher-self-test")
	payload := []byte("musher runner self-test\n")

	if err := os.WriteFile(probe, payload, 0o600); err != nil {
		return fail(s.ScratchDir, err.Error())
	}

	defer removeQuietly(probe)

	got, err := os.ReadFile(probe) //nolint:gosec // G304: our own probe file
	if err != nil || !bytes.Equal(got, payload) {
		return fail(s.ScratchDir, "read back: "+errText(err))
	}

	return pass(s.ScratchDir, "written and read back")
}

func denialKind(err error) string {
	var netErr net.Error

	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "unreachable"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timed out"
	default:
		return err.Error()
	}
}

// apiTarget is host:port of the API URL.
func apiTarget(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}

	if parsed.Port() != "" {
		return parsed.Host
	}

	return net.JoinHostPort(parsed.Hostname(), "443")
}

func withPort(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}

	if _, _, err := net.SplitHostPort(hostport); err == nil {
		return hostport
	}

	return net.JoinHostPort(hostport, "443")
}

func pass(target, detail string) SelfTestCheck {
	return SelfTestCheck{Status: StatusPass, Target: target, Detail: detail}
}

func fail(target, detail string) SelfTestCheck {
	return SelfTestCheck{Status: StatusFail, Target: target, Detail: detail}
}

func errText(err error) string {
	if err == nil {
		return "content differs"
	}

	return err.Error()
}

// WriteSelfTest writes the results to path (when set) and returns the exit
// error: nil when every check passed or was skipped.
func WriteSelfTest(path string, res *SelfTestResults) error {
	if err := writeResults(path, res); err != nil {
		return err
	}

	if !res.Passed {
		failed := []string{}

		for name, c := range res.Checks {
			if c.Status == StatusFail {
				failed = append(failed, name)
			}
		}

		return &clierrors.CLIError{
			Message: "Self-test failed: " + strings.Join(slices.Sorted(slices.Values(failed)), ", "),
			Code:    clierrors.ExitRunnerSelfTestFailed,
		}
	}

	return nil
}
