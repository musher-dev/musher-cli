package runner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/build"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/runner"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

var fastRetry = runner.RetryPolicy{Attempts: 2, Base: time.Millisecond, Max: time.Millisecond}

func newClient(t *testing.T, f *fakeAPI) *runner.Client {
	t.Helper()

	c, err := runner.NewClient(f.URL(), attempt, nil, fastRetry)
	if err != nil {
		t.Fatal(err)
	}

	return c
}

func exitCode(t *testing.T, err error) int {
	t.Helper()

	if err == nil {
		return clierrors.ExitSuccess
	}

	var cliErr *clierrors.CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("error %v is no CLIError", err)
	}

	return cliErr.Code
}

func readResults(t *testing.T, path string) map[string]string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

// --- fetch-source ----------------------------------------------------------

func newFetcher(t *testing.T, f *fakeAPI) *runner.Fetcher {
	t.Helper()

	return &runner.Fetcher{
		Client:       newClient(t, f),
		HTTP:         &http.Client{},
		GitHubAPIURL: f.URL() + "/github",
		ScratchDir:   t.TempDir(),
		Retry:        fastRetry,
		Log:          slog.New(slog.DiscardHandler),
	}
}

func TestFetchSourceStoresAndReportsTheArchive(t *testing.T) {
	t.Parallel()

	f := newFakeAPI(t)
	results := filepath.Join(t.TempDir(), "results.json")

	err := newFetcher(t, f).Run(t.Context(), uuid.MustParse(snapshotID), results)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := f.snapshot()
	if !bytes.Equal(got.uploaded, f.tarball) {
		t.Fatal("storage did not receive the tarball's bytes")
	}

	if got.uploadHeader.Get("X-Amz-Checksum-Sha256") == "" || got.uploadHeader.Get("Content-Length") != strconv.Itoa(len(f.tarball)) {
		t.Fatalf("upload headers = %v, want the checksum and length the API returned", got.uploadHeader)
	}

	if len(got.fetchReports) != 1 {
		t.Fatalf("fetch reports = %d, want 1", len(got.fetchReports))
	}

	report := got.fetchReports[0]
	if report["outcome"] != "FETCHED" || report["commitSha"] != commit || report["contentDigest"] != f.archiveDigest ||
		report["sizeBytes"] != float64(len(f.tarball)) {
		t.Fatalf("report = %v", report)
	}

	for _, token := range got.tokens {
		if token != "Bearer "+attempt {
			t.Fatalf("a runner call carried %q, want the attempt token", token)
		}
	}

	if r := readResults(t, results); r["outcome"] != "FETCHED" || r["exit"] != "reported" {
		t.Fatalf("results = %v", r)
	}
}

func TestFetchSourceClassifiesEveryFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		shape  func(f *fakeAPI)
		reason string
	}{
		{"missing commit", func(f *fakeAPI) { f.tarballCode = http.StatusNotFound }, runner.ReasonCommitNotFound},
		{"unprocessable ref", func(f *fakeAPI) { f.tarballCode = http.StatusUnprocessableEntity }, runner.ReasonCommitNotFound},
		{"token refused", func(f *fakeAPI) { f.tarballCode = http.StatusForbidden }, runner.ReasonRepositoryUnreachable},
		{"forge down", func(f *fakeAPI) { f.tarballCode = http.StatusBadGateway }, runner.ReasonRepositoryUnreachable},
		{"archive too large", func(f *fakeAPI) { f.maxArchive = 10 }, runner.ReasonArchiveTooLarge},
		{"storage refuses", func(f *fakeAPI) { f.uploadCode = http.StatusForbidden }, runner.ReasonFetchFailed},
		{"upload cannot open", func(f *fakeAPI) { f.status["POST :openArchiveUpload"] = http.StatusConflict }, runner.ReasonFetchFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAPI(t)
			tc.shape(f)

			results := filepath.Join(t.TempDir(), "results.json")
			err := newFetcher(t, f).Run(t.Context(), uuid.MustParse(snapshotID), results)

			if code := exitCode(t, err); code != clierrors.ExitRunnerFailureReported {
				t.Fatalf("exit = %d (%v), want %d", code, err, clierrors.ExitRunnerFailureReported)
			}

			got := f.snapshot()
			if len(got.fetchReports) != 1 || got.fetchReports[0]["outcome"] != "FAILED" || got.fetchReports[0]["reason"] != tc.reason {
				t.Fatalf("reports = %v, want one FAILED %s", got.fetchReports, tc.reason)
			}

			if strings.Contains(got.fetchReports[0]["detail"].(string), "ghs_fetchtoken") {
				t.Fatal("the report's detail carries the fetch token")
			}

			if r := readResults(t, results); r["reason"] != tc.reason {
				t.Fatalf("results = %v", r)
			}
		})
	}
}

func TestFetchSourceIsFencedOn401(t *testing.T) {
	t.Parallel()

	for _, route := range []string{"GET /assignment", "POST :openArchiveUpload", "POST :reportFetch"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()

			f := newFakeAPI(t)
			f.status[route] = http.StatusUnauthorized

			err := newFetcher(t, f).Run(t.Context(), uuid.MustParse(snapshotID), "")
			if code := exitCode(t, err); code != clierrors.ExitRunnerFenced {
				t.Fatalf("exit = %d (%v), want fenced", code, err)
			}

			if route != "POST :reportFetch" && len(f.snapshot().fetchReports) != 0 {
				t.Fatal("a fenced attempt reported its fetch")
			}
		})
	}
}

// --- build -----------------------------------------------------------------

// fakeEngine stands in for BuildKit: it reads credentials as a push would,
// writes lines, and returns what the test says.
type fakeEngine struct {
	mu        sync.Mutex
	build     *runner.EngineBuild
	source    string // the Dockerfile the unpacked source held
	passwords []string
	hold      time.Duration // between the first and the second credential read
	waitCtx   bool          // block until ctx ends
	err       error
	digest    string
	lines     []string
}

func (e *fakeEngine) Build(ctx context.Context, b *runner.EngineBuild, auth runner.RegistryAuth, log runner.LogFunc) (string, error) {
	data, _ := os.ReadFile(filepath.Join(b.SourceRoot, b.Context, "Dockerfile"))

	e.mu.Lock()
	e.build = b
	e.source = string(data)
	e.mu.Unlock()

	for _, line := range e.lines {
		log(api.INFO, line, time.Time{})
	}

	read := func() error {
		user, pass, ok, err := auth.Credentials(ctx, "registry.musher.test")
		if err != nil {
			return err //nolint:wrapcheck // the fake hands the grant's error back as the engine would
		}

		if !ok || user != "build" {
			return &runner.EngineError{Kind: runner.KindPushFailed, Err: errors.New("no credentials for the grant host")}
		}

		e.mu.Lock()
		e.passwords = append(e.passwords, pass)
		e.mu.Unlock()

		return nil
	}

	if _, _, ok, _ := auth.Credentials(ctx, "index.docker.io"); ok {
		return "", errors.New("credentials offered for a host the grant is not for")
	}

	if err := read(); err != nil {
		return "", err
	}

	if e.waitCtx {
		<-ctx.Done()

		return "", build.AtStage(build.StageBuild, ctx.Err()) //nolint:wrapcheck // staged as the engine stages
	}

	if e.hold > 0 {
		select {
		case <-time.After(e.hold):
		case <-ctx.Done():
			return "", build.AtStage(build.StageInfrastructure, ctx.Err()) //nolint:wrapcheck // staged as the engine stages
		}

		if err := read(); err != nil {
			return "", err
		}
	}

	if e.err != nil {
		return "", e.err
	}

	return e.digest, nil
}

func newBuilder(t *testing.T, f *fakeAPI, engine runner.Engine) *runner.Builder {
	t.Helper()

	return &runner.Builder{
		Client:      newClient(t, f),
		HTTP:        &http.Client{},
		Engine:      engine,
		ScratchDir:  t.TempDir(),
		Token:       attempt,
		Platform:    "linux/amd64",
		LogInterval: 20 * time.Millisecond,
		Log:         slog.New(slog.DiscardHandler),
	}
}

const pushedDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func TestBuildPushesAndReports(t *testing.T) {
	t.Parallel()

	f := newFakeAPI(t)
	engine := &fakeEngine{digest: pushedDigest, lines: []string{"step one", "token is s3cr3t-value here"}}
	results := filepath.Join(t.TempDir(), "results.json")

	if err := newBuilder(t, f, engine).Run(t.Context(), uuid.MustParse(buildID), results); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := f.snapshot()
	if len(got.buildReports) != 1 || got.buildReports[0]["outcome"] != "PUSHED" || got.buildReports[0]["imageDigest"] != pushedDigest {
		t.Fatalf("reports = %v", got.buildReports)
	}

	b := engine.build
	if b.Kind != build.KindDockerfile || b.Dockerfile != "Dockerfile" || b.Repository != "registry.musher.test/org-1/src-42" ||
		b.Tag != "c0ffee" || string(b.Secrets["NPM_TOKEN"]) != "s3cr3t-value" || b.Arguments["A"] != "1" {
		t.Fatalf("engine got %+v", b)
	}

	if engine.source != "FROM scratch\n" {
		t.Fatalf("the engine saw Dockerfile %q: the source was not unpacked without its top directory", engine.source)
	}

	assertLogLines(t, got.logLines)

	if r := readResults(t, results); r["outcome"] != "PUSHED" || r["exit"] != "reported" || len(r) != 2 {
		t.Fatalf("results = %v (the digest travels only on :reportResult)", r)
	}
}

// assertLogLines holds the batch contract: seq from 0 with no gap, every
// line timestamped and severe, and no secret value anywhere.
func assertLogLines(t *testing.T, lines []map[string]any) {
	t.Helper()

	if len(lines) == 0 {
		t.Fatal("no log lines were sent")
	}

	for i, line := range lines {
		if line["seq"] != float64(i) {
			t.Fatalf("line %d has seq %v", i, line["seq"])
		}

		if line["occurredAt"] == "" || line["severity"] == "" {
			t.Fatalf("line %d = %v", i, line)
		}

		msg, _ := line["message"].(string)
		if strings.Contains(msg, "s3cr3t-value") || strings.Contains(msg, "push-pass") || strings.Contains(msg, attempt) {
			t.Fatalf("line %d leaks a secret: %q", i, msg)
		}
	}
}

func TestBuildClassifiesEveryFailure(t *testing.T) {
	t.Parallel()

	unsafeArchive := tarGz(t, map[string]string{"repo/../../etc/passwd": "x"})

	cases := []struct {
		name   string
		shape  func(f *fakeAPI, e *fakeEngine)
		kind   string
		detail string
	}{
		{"context escapes", func(f *fakeAPI, _ *fakeEngine) { f.recipe["context"] = "../up" }, runner.KindRecipeInvalid, "leaves the source"},
		{"unpinned builder", func(f *fakeAPI, _ *fakeEngine) {
			f.recipe["kind"], f.recipe["builderImage"] = "BUILDPACKS", "paketobuildpacks/builder-jammy-base"
		}, runner.KindRecipeInvalid, "not pinned"},
		{"secret without value", func(f *fakeAPI, _ *fakeEngine) { f.secrets = map[string]string{} }, runner.KindRecipeInvalid, "NPM_TOKEN"},
		{"engine recipe error", func(_ *fakeAPI, e *fakeEngine) {
			e.err = build.AtStage(build.StageRecipe, errors.New("dockerfile parse error"))
		}, runner.KindRecipeInvalid, "parse error"},
		{"RUN step fails", func(_ *fakeAPI, e *fakeEngine) {
			e.err = build.AtStage(build.StageBuild, errors.New("process exited 1 with s3cr3t-value"))
		}, runner.KindBuildFailed, "[REDACTED]"},
		{"registry refuses", func(_ *fakeAPI, e *fakeEngine) {
			e.err = build.AtStage(build.StagePush, errors.New("push access denied"))
		}, runner.KindPushFailed, "denied"},
		{"engine lost", func(_ *fakeAPI, e *fakeEngine) { e.err = errors.New("buildkitd went away") }, runner.KindInfrastructure, "went away"},
		{"no digest", func(_ *fakeAPI, e *fakeEngine) { e.digest = "" }, runner.KindPushFailed, "no manifest digest"},
		{"archive digest differs", func(f *fakeAPI, _ *fakeEngine) { f.archiveDigest = "sha256:" + strings.Repeat("0", 64) }, runner.KindInfrastructure, "not the assigned one"},
		{"unsafe archive", func(f *fakeAPI, _ *fakeEngine) { f.setArchive(unsafeArchive) }, runner.KindBuildFailed, "climbs out"},
		{"grant refused", func(f *fakeAPI, _ *fakeEngine) { f.status["POST :grantPushAccess"] = http.StatusServiceUnavailable }, runner.KindInfrastructure, "push access"},
		{"malformed assignment", func(f *fakeAPI, _ *fakeEngine) { f.recipe = nil }, runner.KindInfrastructure, "no recipe"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAPI(t)
			engine := &fakeEngine{digest: pushedDigest}
			tc.shape(f, engine)

			err := newBuilder(t, f, engine).Run(t.Context(), uuid.MustParse(buildID), "")
			if code := exitCode(t, err); code != clierrors.ExitRunnerFailureReported {
				t.Fatalf("exit = %d (%v), want %d", code, err, clierrors.ExitRunnerFailureReported)
			}

			got := f.snapshot()
			if len(got.buildReports) != 1 {
				t.Fatalf("reports = %v, want one", got.buildReports)
			}

			report := got.buildReports[0]
			detail, _ := report["detail"].(string)

			if report["outcome"] != "FAILED" || report["kind"] != tc.kind || !strings.Contains(detail, tc.detail) {
				t.Fatalf("report = %v, want FAILED %s mentioning %q", report, tc.kind, tc.detail)
			}

			if strings.Contains(detail, "s3cr3t-value") {
				t.Fatal("the report's detail carries a secret value")
			}
		})
	}
}

func TestBuildIsFencedOn401(t *testing.T) {
	t.Parallel()

	cases := map[string]func(f *fakeAPI){
		"assignment":   func(f *fakeAPI) { f.status["GET /assignment"] = http.StatusUnauthorized },
		"grant":        func(f *fakeAPI) { f.status["POST :grantPushAccess"] = http.StatusUnauthorized },
		"log-lines":    func(f *fakeAPI) { f.logStatusAt, f.logStatus = 1, http.StatusUnauthorized },
		"not this one": func(f *fakeAPI) { f.status["GET /assignment"] = http.StatusNotFound },
	}

	for name, shape := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAPI(t)
			shape(f)

			// The engine holds until it is stopped, so only fencing ends it.
			engine := &fakeEngine{digest: pushedDigest, waitCtx: true, lines: []string{"working"}}

			done := make(chan error, 1)

			go func() { done <- newBuilder(t, f, engine).Run(t.Context(), uuid.MustParse(buildID), "") }()

			select {
			case err := <-done:
				if code := exitCode(t, err); code != clierrors.ExitRunnerFenced {
					t.Fatalf("exit = %d (%v), want fenced", code, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("a fenced build kept running")
			}

			if reports := f.snapshot().buildReports; len(reports) != 0 {
				t.Fatalf("a fenced attempt reported: %v", reports)
			}
		})
	}
}

func TestBuildRefreshesThePushGrantDuringALongBuild(t *testing.T) {
	t.Parallel()

	f := newFakeAPI(t)
	// A 6s grant is renewed 5s before it ends, so a build that pushes 2s in
	// must be handed a second one.
	f.grantTTL = 6 * time.Second
	engine := &fakeEngine{digest: pushedDigest, hold: 2 * time.Second}

	if err := newBuilder(t, f, engine).Run(t.Context(), uuid.MustParse(buildID), ""); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := f.snapshot()
	if got.grants < 2 {
		t.Fatalf("grants issued = %d, want a renewal during the build", got.grants)
	}

	if len(engine.passwords) != 2 || engine.passwords[0] == engine.passwords[1] {
		t.Fatalf("the push read %v, want the renewed grant's password the second time", engine.passwords)
	}

	assertLogLines(t, got.logLines)
}

func TestBuildStopsBeforeItsDeadline(t *testing.T) {
	t.Parallel()

	f := newFakeAPI(t)
	// 16s leaves 1s of work before the 15s report margin.
	f.deadline = time.Now().Add(16 * time.Second)
	engine := &fakeEngine{waitCtx: true}

	start := time.Now()
	err := newBuilder(t, f, engine).Run(t.Context(), uuid.MustParse(buildID), "")

	if code := exitCode(t, err); code != clierrors.ExitRunnerDeadline {
		t.Fatalf("exit = %d (%v), want deadline", code, err)
	}

	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the build ran %s, past its working time", elapsed)
	}

	if reports := f.snapshot().buildReports; len(reports) != 0 {
		t.Fatalf("a build cut short by its deadline reported: %v", reports)
	}
}

func TestReportUndeliveredExits23(t *testing.T) {
	t.Parallel()

	f := newFakeAPI(t)
	f.status["POST :reportResult"] = http.StatusServiceUnavailable

	err := newBuilder(t, f, &fakeEngine{digest: pushedDigest}).Run(t.Context(), uuid.MustParse(buildID), "")
	if code := exitCode(t, err); code != clierrors.ExitRunnerUnreported {
		t.Fatalf("exit = %d (%v), want %d", code, err, clierrors.ExitRunnerUnreported)
	}
}
