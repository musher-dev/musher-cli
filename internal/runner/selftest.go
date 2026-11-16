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
)

// SelfTestSchemaVersion is the version of the self-test results document,
// contracts/runner/self-test.schema.json, which host-agent's canary parses.
// It changes only when a key is removed or changes meaning.
const SelfTestSchemaVersion = 1

// Check names, the keys of SelfTestResults.Checks. Fixed with host-agent's
// canary, which maps each to an isolation condition.
const (
	CheckBuildEngine    = "buildEngine"
	CheckLocalBuild     = "localBuild"
	CheckAPIEgress      = "apiEgress"
	CheckRegistryEgress = "registryEgress"
	CheckDeniedEgress   = "deniedEgress"
	CheckScratch        = "scratch"
)

const (
	defaultDenyTimeout    = 3 * time.Second
	defaultReachTimeout   = 10 * time.Second
	defaultBuildKitBudget = 60 * time.Second
)

// SelfTestCheck is one check's verdict, as the canary reads it.
type SelfTestCheck struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// SelfTestResults is the document `musher runner self-test` writes.
type SelfTestResults struct {
	SchemaVersion int                      `json:"schemaVersion"`
	RunnerVersion string                   `json:"runnerVersion"`
	Checks        map[string]SelfTestCheck `json:"checks"`
}

// Passed reports whether every check is ok.
func (r *SelfTestResults) Passed() bool {
	for _, c := range r.Checks {
		if !c.OK {
			return false
		}
	}

	return true
}

// Failed names the checks that are not ok, sorted.
func (r *SelfTestResults) Failed() []string {
	failed := []string{}

	for name, c := range r.Checks {
		if !c.OK {
			failed = append(failed, name)
		}
	}

	return slices.Sorted(slices.Values(failed))
}

// probe is one probe's outcome before it is folded into a check.
type probe struct {
	ok      bool
	target  string
	detail  string
	elapsed time.Duration
}

func (p probe) check() SelfTestCheck {
	detail := p.detail
	if p.target != "" {
		detail = p.target + ": " + detail
	}

	return SelfTestCheck{OK: p.ok, Detail: detail + " (" + p.elapsed.Round(time.Millisecond).String() + ")"}
}

// SelfTest is the guest canary's in-guest half.
type SelfTest struct {
	Version      string
	BuildKitHost string
	APIURL       string
	// RegistryHost is host[:port]; empty fails the registry check.
	RegistryHost string
	ScratchDir   string
	// Deny targets (host:port) that must not be reachable; an empty one is
	// left out.
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

	checks := map[string]func(ctx context.Context) SelfTestCheck{
		CheckBuildEngine:    timed(s.checkWorkers),
		CheckLocalBuild:     timed(s.checkScratchBuild),
		CheckAPIEgress:      timed(func(ctx context.Context) probe { return s.checkReach(ctx, apiTarget(s.APIURL), "API URL") }),
		CheckRegistryEgress: timed(func(ctx context.Context) probe { return s.checkReach(ctx, withPort(s.RegistryHost), "registry") }),
		CheckDeniedEgress:   s.checkAllDenied,
		CheckScratch:        timed(s.checkScratch),
	}

	res := &SelfTestResults{
		SchemaVersion: SelfTestSchemaVersion,
		RunnerVersion: s.Version,
		Checks:        make(map[string]SelfTestCheck, len(checks)),
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for name, check := range checks {
		wg.Go(func() {
			verdict := check(ctx)

			mu.Lock()
			res.Checks[name] = verdict
			mu.Unlock()
		})
	}

	wg.Wait()

	return res
}

func timed(run func(ctx context.Context) probe) func(ctx context.Context) SelfTestCheck {
	return func(ctx context.Context) SelfTestCheck {
		start := time.Now()
		p := run(ctx)
		p.elapsed = time.Since(start)

		return p.check()
	}
}

// checkAllDenied runs every deny probe at once; it is ok only when none of
// them connected.
func (s *SelfTest) checkAllDenied(ctx context.Context) SelfTestCheck {
	targets := []string{}

	for _, t := range []string{s.DenyMetadata, s.DenyPrivate, s.DenyCGNAT} {
		if strings.TrimSpace(t) != "" {
			targets = append(targets, t)
		}
	}

	if len(targets) == 0 {
		return SelfTestCheck{OK: false, Detail: "no deny targets configured"}
	}

	results := make([]probe, len(targets))

	var wg sync.WaitGroup

	for i, target := range targets {
		wg.Go(func() {
			start := time.Now()
			results[i] = s.checkDenied(ctx, target)
			results[i].elapsed = time.Since(start)
		})
	}

	wg.Wait()

	ok := true
	details := make([]string, 0, len(results))

	for _, p := range results {
		ok = ok && p.ok
		details = append(details, p.check().Detail)
	}

	return SelfTestCheck{OK: ok, Detail: strings.Join(details, "; ")}
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

func (s *SelfTest) checkWorkers(ctx context.Context) probe {
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

func (s *SelfTest) checkScratchBuild(ctx context.Context) probe {
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

// checkReach proves TCP and a verified TLS handshake to target; what names
// the kind of target, for the detail when none is configured.
func (s *SelfTest) checkReach(ctx context.Context, target, what string) probe {
	if target == "" {
		return fail("", "no "+what+" configured")
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
func (s *SelfTest) checkDenied(ctx context.Context, target string) probe {
	ctx, cancel := context.WithTimeout(ctx, s.DenyTimeout)
	defer cancel()

	conn, err := s.Dial(ctx, "tcp", target)
	if err == nil {
		closeQuietly(conn)

		return fail(target, "CONNECTED: egress to this address is not denied")
	}

	return pass(target, "denied: "+denialKind(err))
}

func (s *SelfTest) checkScratch(_ context.Context) probe {
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

func pass(target, detail string) probe {
	return probe{ok: true, target: target, detail: detail}
}

func fail(target, detail string) probe {
	return probe{ok: false, target: target, detail: detail}
}

func errText(err error) string {
	if err == nil {
		return "content differs"
	}

	return err.Error()
}

// WriteSelfTest writes the results to path (when set). A failed check is
// not an error: the canary reads the verdicts from the file, so the command
// exits 0 whenever the file was written, and fails only when it cannot be.
func WriteSelfTest(path string, res *SelfTestResults) error {
	return writeResults(path, res)
}
