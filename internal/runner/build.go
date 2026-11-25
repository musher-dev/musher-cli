package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/build"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// Builder runs `musher runner build`.
type Builder struct {
	Client *Client
	// HTTP downloads the source archive from its presigned URL. It never
	// carries the attempt token.
	HTTP   *http.Client
	Engine Engine
	// ScratchDir holds the archive, the extracted source and engine state.
	ScratchDir string
	// Token is the attempt token, redacted from everything the build writes.
	Token string
	// TokenExpiry bounds the whole command; zero means none.
	TokenExpiry time.Time
	// Platform is the target platform (linux/amd64).
	Platform string
	// LogInterval is how often build output is sent.
	LogInterval time.Duration
	Limits      ExtractLimits
	Log         *slog.Logger
}

// buildError is a classified reason the build failed.
type buildError struct {
	kind string
	err  error
}

func (e *buildError) Error() string { return e.kind + ": " + e.err.Error() }
func (e *buildError) Unwrap() error { return e.err }

func failBuild(kind string, err error) error { return &buildError{kind: kind, err: err} }

// fence records the first fenced answer any part of the build saw and stops
// the rest of it.
type fence struct {
	once   sync.Once
	err    error
	cancel context.CancelFunc
}

func (f *fence) trip(err error) {
	f.once.Do(func() {
		f.err = err
		f.cancel()
	})
}

// Run builds image build id and reports the outcome.
func (b *Builder) Run(ctx context.Context, id uuid.UUID, resultsFile string) error {
	results := Results{}

	if !b.TokenExpiry.IsZero() {
		var cancel context.CancelFunc

		ctx, cancel = context.WithDeadline(ctx, b.TokenExpiry)
		defer cancel()
	}

	err := b.run(ctx, id, results)

	return endWith(resultsFile, results, err)
}

func (b *Builder) run(ctx context.Context, id uuid.UUID, results Results) error {
	raw, err := b.Client.ImageBuildAssignment(ctx, id)
	if err != nil {
		return err
	}

	redact := NewRedactor(b.Token)

	assignment, err := checkBuildAssignment(raw)
	if err != nil {
		return b.report(ctx, id, results, redact, "", failBuild(KindInfrastructure, err))
	}

	for _, v := range assignment.Secrets {
		redact.Add(v)
	}

	// Never run past the attempt's deadline.
	ctx, cancelDeadline := context.WithDeadline(ctx, assignment.DeadlineAt)
	defer cancelDeadline()

	fenceCtx, cancelFence := context.WithCancel(ctx)
	defer cancelFence()

	fenced := &fence{cancel: cancelFence}

	logs := NewLogShipper(b.Client, id, redact, b.LogInterval, fenced.trip)
	logsCtx, stopLogs := context.WithCancel(fenceCtx)

	go logs.Run(logsCtx, fenceCtx)

	workCtx, cancelWork := withMargin(fenceCtx)
	digest, buildErr := b.build(workCtx, id, assignment, redact, logs, fenced)

	cancelWork()

	if buildErr != nil {
		logs.Add(api.ERROR, "build failed: "+buildErr.Error(), time.Time{})
	} else {
		logs.Add(api.INFO, "pushed "+assignment.ImageRepository+":"+assignment.ImageTag+"@"+digest, time.Time{})
	}

	// Send every line before the result: once the result is in, the attempt
	// may end and its token with it.
	stopLogs()
	logs.Wait()

	if fenced.err != nil {
		return fenced.err
	}

	if buildErr != nil && errors.Is(workCtx.Err(), context.DeadlineExceeded) {
		// The deadline (less the report margin) cut the build short. The
		// platform times the attempt out; a report now would only race it.
		if ctx.Err() == nil {
			return context.DeadlineExceeded
		}

		return ctx.Err() //nolint:wrapcheck // tested for the deadline
	}

	return b.report(ctx, id, results, redact, digest, buildErr)
}

func (b *Builder) build(ctx context.Context, id uuid.UUID, assignment *BuildAssignment, redact *Redactor, logs *LogShipper, fenced *fence) (string, error) {
	if err := checkRecipe(assignment); err != nil {
		return "", failBuild(KindRecipeInvalid, err)
	}

	work, err := os.MkdirTemp(b.ScratchDir, "build-")
	if err != nil {
		return "", failBuild(KindInfrastructure, clierrors.Errorf("create scratch directory: %w", err))
	}

	defer removeQuietly(work)

	logs.Add(api.INFO, "downloading source archive "+assignment.ArchiveDigest+" (commit "+assignment.CommitSHA+")", time.Time{})

	archive := filepath.Join(work, "source.tar.gz")
	if dlErr := b.download(ctx, assignment, archive); dlErr != nil {
		return "", dlErr
	}

	source := filepath.Join(work, "src")
	if exErr := b.extract(archive, source); exErr != nil {
		return "", exErr
	}

	grants := NewGrantSource(b.Client, id, fenced.trip, func(g *PushGrant) { redact.Add(g.Password) })

	grant, err := grants.Refresh(ctx)
	if err != nil {
		if IsFenced(err) {
			return "", err
		}

		return "", failBuild(KindInfrastructure, clierrors.Errorf("push access: %w", err))
	}

	if coverErr := grantCovers(grant, assignment.ImageRepository); coverErr != nil {
		return "", failBuild(KindInfrastructure, coverErr)
	}

	go grants.Keep(ctx)

	engineDir := filepath.Join(work, "engine")
	if mkErr := os.MkdirAll(engineDir, 0o750); mkErr != nil {
		return "", failBuild(KindInfrastructure, clierrors.Errorf("create %s: %w", engineDir, mkErr))
	}

	logs.Add(api.INFO, "building "+strings.ToLower(assignment.Kind)+" recipe for "+assignment.ImageRepository+":"+assignment.ImageTag, time.Time{})

	digest, err := b.Engine.Build(ctx, &EngineBuild{
		Kind:         engineKind(assignment.Kind),
		SourceRoot:   source,
		Context:      orDot(assignment.Context),
		Dockerfile:   assignment.DockerfilePath,
		BuilderImage: assignment.BuilderImage,
		Arguments:    assignment.Arguments,
		Secrets:      secretBytes(assignment),
		Repository:   assignment.ImageRepository,
		Tag:          assignment.ImageTag,
		Platform:     b.Platform,
		WorkDir:      engineDir,
	}, grants, logs.Add)
	if err != nil {
		if fenced.err != nil {
			return "", fenced.err
		}

		return "", failBuild(FailureKind(err), err)
	}

	if !sha256Digest.MatchString(digest) {
		return "", failBuild(KindPushFailed, clierrors.Errorf("the registry returned no manifest digest (%q)", digest))
	}

	return digest, nil
}

// download fetches the archive and refuses any bytes but the assigned ones.
func (b *Builder) download(ctx context.Context, assignment *BuildAssignment, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assignment.ArchiveURL, http.NoBody)
	if err != nil {
		return failBuild(KindInfrastructure, clierrors.Errorf("build archive request: %w", err))
	}

	resp, err := b.HTTP.Do(req)
	if err != nil {
		return failBuild(KindInfrastructure, clierrors.Errorf("download the source archive: %w", err))
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return failBuild(KindInfrastructure, clierrors.Errorf("download the source archive: HTTP %d", resp.StatusCode))
	}

	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: our own scratch directory
	if err != nil {
		return failBuild(KindInfrastructure, clierrors.Errorf("create %s: %w", path, err))
	}

	defer closeQuietly(out)

	hash := sha256.New()

	written, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, assignment.ArchiveSizeBytes+1))
	if err != nil {
		return failBuild(KindInfrastructure, clierrors.Errorf("download the source archive: %w", err))
	}

	got := "sha256:" + hex.EncodeToString(hash.Sum(nil))

	if written != assignment.ArchiveSizeBytes || got != assignment.ArchiveDigest {
		return failBuild(KindInfrastructure, clierrors.Errorf("the source archive is not the assigned one: %d bytes %s, want %d bytes %s", written, got, assignment.ArchiveSizeBytes, assignment.ArchiveDigest))
	}

	if err := out.Close(); err != nil {
		return failBuild(KindInfrastructure, clierrors.Errorf("write %s: %w", path, err))
	}

	return nil
}

func (b *Builder) extract(archive, dest string) error {
	f, err := os.Open(archive) //nolint:gosec // G304: our own scratch directory
	if err != nil {
		return failBuild(KindInfrastructure, clierrors.Errorf("open %s: %w", archive, err))
	}

	defer closeQuietly(f)

	limits := b.Limits
	if limits.MaxBytes == 0 {
		limits = DefaultExtractLimits
	}

	if err := ExtractTarGz(f, dest, limits); err != nil {
		var unsafe *UnsafeArchiveError
		if errors.As(err, &unsafe) {
			return failBuild(KindBuildFailed, err)
		}

		return failBuild(KindInfrastructure, err)
	}

	return nil
}

// report sends PUSHED or a classified FAILED.
func (b *Builder) report(ctx context.Context, id uuid.UUID, results Results, redact *Redactor, digest string, cause error) error {
	if IsFenced(cause) {
		return cause
	}

	var body api.RunnerReportImageBuildResultJSONBody

	if cause == nil {
		results[ResultOutcome] = "PUSHED"

		if err := body.FromPushedReport(api.PushedReport{Outcome: "PUSHED", ImageDigest: digest}); err != nil {
			return clierrors.Errorf("encode build report: %w", err)
		}
	} else {
		kind := KindInfrastructure

		var failure *buildError
		if errors.As(cause, &failure) {
			kind = failure.kind
		}

		results[ResultOutcome] = "FAILED"
		results[ResultReason] = kind

		detail := truncateDetail(redact.Redact(cause.Error()))
		b.Log.Error("build failed", "kind", kind, "error", detail)

		if err := body.FromFailedBuildReport(api.FailedBuildReport{
			Outcome: "FAILED",
			Kind:    api.FailedBuildReportKind(kind),
			Detail:  &detail,
		}); err != nil {
			return clierrors.Errorf("encode build report: %w", err)
		}
	}

	if _, err := b.Client.ReportResult(ctx, id, body); err != nil {
		return err
	}

	if cause != nil {
		return &reportedError{Kind: results[ResultReason], Detail: redact.Redact(cause.Error())}
	}

	return nil
}

// grantCovers checks the grant reaches the repository the build pushes to.
func grantCovers(g *PushGrant, repository string) error {
	host := registryHostOf(repository)
	path := strings.TrimPrefix(repository, host+"/")

	if !sameHost(g.RegistryHost, host) || (g.RepositoryPath != "" && g.RepositoryPath != path) {
		return clierrors.Errorf("the push grant is for %s/%s, not %s", g.RegistryHost, g.RepositoryPath, repository)
	}

	return nil
}

func engineKind(kind string) build.Kind {
	if kind == RecipeBuildpacks {
		return build.KindBuildpacks
	}

	return build.KindDockerfile
}

func secretBytes(a *BuildAssignment) map[string][]byte {
	out := make(map[string][]byte, len(a.Secrets))
	for id, v := range a.Secrets {
		out[id] = []byte(v)
	}

	return out
}

func orDot(s string) string {
	if s == "" {
		return "."
	}

	return s
}
