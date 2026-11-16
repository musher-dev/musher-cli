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
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// Fetch failure reasons, the API's FailedFetchReport.reason.
const (
	ReasonCommitNotFound        = "COMMIT_NOT_FOUND"
	ReasonRepositoryUnreachable = "REPOSITORY_UNREACHABLE"
	ReasonArchiveTooLarge       = "ARCHIVE_TOO_LARGE"
	ReasonFetchFailed           = "FETCH_FAILED"
)

// Fetcher runs `musher runner fetch-source`.
type Fetcher struct {
	Client *Client
	// HTTP downloads from the forge and uploads to storage. It never carries
	// the attempt token.
	HTTP *http.Client
	// GitHubAPIURL overrides the forge API derived from the clone URL (tests).
	GitHubAPIURL string
	// ScratchDir holds the downloaded archive.
	ScratchDir string
	// TokenExpiry bounds the whole command; zero means none.
	TokenExpiry time.Time
	// Retry bounds forge and storage retries.
	Retry RetryPolicy
	Log   *slog.Logger

	redact *Redactor
}

// fetchError is a classified reason the fetch failed.
type fetchError struct {
	reason string
	err    error
}

func (e *fetchError) Error() string { return e.reason + ": " + e.err.Error() }
func (e *fetchError) Unwrap() error { return e.err }

func failFetch(reason string, err error) error { return &fetchError{reason: reason, err: err} }

// Run fetches snapshot id's commit and reports the outcome.
func (f *Fetcher) Run(ctx context.Context, id uuid.UUID, resultsFile string) error {
	results := Results{}

	if !f.TokenExpiry.IsZero() {
		var cancel context.CancelFunc

		ctx, cancel = context.WithDeadline(ctx, f.TokenExpiry)
		defer cancel()
	}

	err := f.run(ctx, id, results)

	return endWith(resultsFile, results, err)
}

func (f *Fetcher) run(ctx context.Context, id uuid.UUID, results Results) error {
	raw, err := f.Client.SourceSnapshotAssignment(ctx, id)
	if err != nil {
		return err
	}

	assignment, err := checkSnapshotAssignment(raw)
	if assignment != nil {
		f.redact = NewRedactor(assignment.FetchToken)
	}

	if err != nil {
		return f.report(ctx, id, results, nil, failFetch(ReasonFetchFailed, err))
	}

	// Leave time to report before the token ends.
	workCtx, cancel := withMargin(ctx)
	defer cancel()

	archive, err := f.fetch(workCtx, id, assignment)
	if err != nil && ctx.Err() == nil && workCtx.Err() != nil {
		err = failFetch(ReasonFetchFailed, clierrors.Errorf("the fetch did not finish before the attempt's deadline"))
	}

	return f.report(ctx, id, results, archive, err)
}

// fetchedArchive is the archive the fetch stored.
type fetchedArchive struct {
	commit string
	sha256 string
	size   int64
}

func (f *Fetcher) fetch(ctx context.Context, id uuid.UUID, assignment *SnapshotAssignment) (*fetchedArchive, error) {
	dir, err := os.MkdirTemp(f.ScratchDir, "fetch-")
	if err != nil {
		return nil, failFetch(ReasonFetchFailed, clierrors.Errorf("create scratch directory: %w", err))
	}

	defer removeQuietly(dir)

	path := filepath.Join(dir, "source.tar.gz")

	sum, size, err := f.download(ctx, assignment, path)
	if err != nil {
		return nil, err
	}

	f.Log.Info("downloaded commit archive", "commit", assignment.CommitSHA, "size_bytes", size, "sha256", sum)

	upload, err := f.Client.OpenArchiveUpload(ctx, id, api.OpenArchiveUploadRequest{Sha256: sum, SizeBytes: int(size)})
	if err != nil {
		return nil, err
	}

	if upErr := f.upload(ctx, upload, path, size); upErr != nil {
		return nil, upErr
	}

	return &fetchedArchive{commit: assignment.CommitSHA, sha256: sum, size: size}, nil
}

// download streams the commit tarball into path, hashing it and refusing
// more than the assignment's maxArchiveBytes.
func (f *Fetcher) download(ctx context.Context, assignment *SnapshotAssignment, path string) (sum string, size int64, err error) {
	base := f.GitHubAPIURL
	if base == "" {
		base = gitHubAPIBase(assignment.CloneURL)
	}

	url := strings.TrimRight(base, "/") + "/repos/" + assignment.FullName + "/tarball/" + assignment.CommitSHA
	backoff := f.Retry.Base

	for attempt := 1; ; attempt++ {
		var retry bool

		sum, size, retry, err = f.downloadOnce(ctx, url, assignment, path)
		if err == nil {
			return sum, size, nil
		}

		if !retry || attempt >= max(f.Retry.Attempts, 1) || ctx.Err() != nil {
			return "", 0, err
		}

		if sleepErr := sleepCtx(ctx, backoff); sleepErr != nil {
			return "", 0, err
		}

		backoff = min(backoff*2, f.Retry.Max)
	}
}

func (f *Fetcher) downloadOnce(ctx context.Context, url string, assignment *SnapshotAssignment, path string) (sum string, size int64, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", 0, false, failFetch(ReasonFetchFailed, clierrors.Errorf("build tarball request: %w", err))
	}

	repo, sha, limit := assignment.FullName, assignment.CommitSHA, assignment.MaxArchiveBytes

	req.Header.Set("Authorization", "Bearer "+assignment.FetchToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-Github-Api-Version", "2022-11-28")

	resp, err := f.HTTP.Do(req)
	if err != nil {
		return "", 0, true, failFetch(ReasonRepositoryUnreachable, clierrors.Errorf("download %s: %w", repo, err))
	}

	defer closeQuietly(resp.Body)

	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity:
		return "", 0, false, failFetch(ReasonCommitNotFound, clierrors.Errorf("%s has no commit %s (HTTP %d)", repo, sha, resp.StatusCode))
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", 0, false, failFetch(ReasonRepositoryUnreachable, clierrors.Errorf("the fetch token cannot read %s (HTTP %d)", repo, resp.StatusCode))
	case resp.StatusCode != http.StatusOK:
		return "", 0, retryable(resp.StatusCode), failFetch(ReasonRepositoryUnreachable, clierrors.Errorf("download %s: HTTP %d", repo, resp.StatusCode))
	}

	if resp.ContentLength > limit {
		return "", 0, false, failFetch(ReasonArchiveTooLarge, clierrors.Errorf("the archive is %d bytes; the limit is %d", resp.ContentLength, limit))
	}

	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: a path in our own scratch directory
	if err != nil {
		return "", 0, false, failFetch(ReasonFetchFailed, clierrors.Errorf("create %s: %w", path, err))
	}

	defer closeQuietly(out)

	hash := sha256.New()

	written, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return "", 0, true, failFetch(ReasonRepositoryUnreachable, clierrors.Errorf("download %s: %w", repo, err))
	}

	if written > limit {
		return "", 0, false, failFetch(ReasonArchiveTooLarge, clierrors.Errorf("the archive exceeds the limit of %d bytes", limit))
	}

	if written == 0 {
		return "", 0, false, failFetch(ReasonFetchFailed, clierrors.Errorf("the forge returned an empty archive"))
	}

	if err := out.Close(); err != nil {
		return "", 0, false, failFetch(ReasonFetchFailed, clierrors.Errorf("write %s: %w", path, err))
	}

	return hex.EncodeToString(hash.Sum(nil)), written, false, nil
}

// upload PUTs the archive to the presigned URL with exactly the headers the
// API returned; their checksum and length are what storage signed.
func (f *Fetcher) upload(ctx context.Context, upload *api.ArchiveUploadResponse, path string, size int64) error {
	if upload.UploadUrl == nil || *upload.UploadUrl == "" {
		return failFetch(ReasonFetchFailed, clierrors.Errorf("the archive upload has no uploadUrl"))
	}

	method := http.MethodPut
	if upload.Method != nil && *upload.Method != "" {
		method = strings.ToUpper(*upload.Method)
	}

	headers := derefMap(upload.Headers)

	var lastErr error

	for attempt, backoff := 1, f.Retry.Base; ; attempt++ {
		retry, err := f.uploadOnce(ctx, method, *upload.UploadUrl, headers, path, size)
		if err == nil {
			return nil
		}

		lastErr = err

		if !retry || attempt >= max(f.Retry.Attempts, 1) || ctx.Err() != nil {
			return lastErr
		}

		if err := sleepCtx(ctx, backoff); err != nil {
			return lastErr
		}

		backoff = min(backoff*2, f.Retry.Max)
	}
}

func (f *Fetcher) uploadOnce(ctx context.Context, method, url string, headers map[string]string, path string, size int64) (bool, error) {
	body, err := os.Open(path) //nolint:gosec // G304: a path in our own scratch directory
	if err != nil {
		return false, failFetch(ReasonFetchFailed, clierrors.Errorf("open %s: %w", path, err))
	}

	defer closeQuietly(body)

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return false, failFetch(ReasonFetchFailed, clierrors.Errorf("build upload request: %w", err))
	}

	req.ContentLength = size

	for key, value := range headers {
		switch strings.ToLower(key) {
		case "content-length":
			declared, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil || declared != size {
				return false, failFetch(ReasonFetchFailed, clierrors.Errorf("the upload expects %s bytes; the archive has %d", value, size))
			}
		case "host":
			req.Host = value
		default:
			req.Header.Set(key, value)
		}
	}

	resp, err := f.HTTP.Do(req)
	if err != nil {
		return true, failFetch(ReasonFetchFailed, clierrors.Errorf("upload the archive: %w", err))
	}

	defer resp.Body.Close()

	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)); err != nil {
		slog.Debug("drain the upload answer", "error", err)
	}

	if resp.StatusCode/100 != 2 {
		return retryable(resp.StatusCode), failFetch(ReasonFetchFailed, clierrors.Errorf("storage refused the archive upload: HTTP %d", resp.StatusCode))
	}

	return false, nil
}

// report sends FETCHED or a classified FAILED. A fenced error, or the
// deadline, is passed through: the API cannot accept anything any more.
func (f *Fetcher) report(ctx context.Context, id uuid.UUID, results Results, archive *fetchedArchive, cause error) error {
	if IsFenced(cause) {
		return cause
	}

	if cause != nil && ctx.Err() != nil {
		return context.DeadlineExceeded
	}

	var body api.RunnerReportSourceSnapshotFetchJSONBody

	if cause == nil {
		results[ResultOutcome] = "FETCHED"

		if err := body.FromFetchedReport(api.FetchedReport{
			Outcome:       "FETCHED",
			CommitSha:     archive.commit,
			ContentDigest: "sha256:" + archive.sha256,
			SizeBytes:     int(archive.size),
		}); err != nil {
			return clierrors.Errorf("encode fetch report: %w", err)
		}
	} else {
		reason := ReasonFetchFailed

		var failure *fetchError
		if errors.As(cause, &failure) {
			reason = failure.reason
		}

		results[ResultOutcome] = "FAILED"
		results[ResultReason] = reason

		f.Log.Error("fetch failed", "reason", reason, "error", f.redact.Redact(cause.Error()))

		detail := truncateDetail(f.redact.Redact(cause.Error()))
		if err := body.FromFailedFetchReport(api.FailedFetchReport{
			Outcome: "FAILED",
			Reason:  api.FailedFetchReportReason(reason),
			Detail:  &detail,
		}); err != nil {
			return clierrors.Errorf("encode fetch report: %w", err)
		}
	}

	if _, err := f.Client.ReportFetch(ctx, id, body); err != nil {
		return err
	}

	if cause != nil {
		return &reportedError{Kind: results[ResultReason], Detail: cause.Error()}
	}

	return nil
}

// withMargin is ctx ending reportMargin before its deadline.
func withMargin(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}

	return context.WithDeadline(ctx, deadline.Add(-reportMargin))
}
