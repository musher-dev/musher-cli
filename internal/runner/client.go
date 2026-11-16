package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/musher-dev/musher-cli/internal/buildinfo"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// Operation is one runner-surface operation the runner calls, by its
// operationId in the vendored runner.json. contract_test.go holds this list
// to the spec and the generated client.
type Operation string

// The operations runner mode calls. Each speaks with the run attempt token
// only; nothing else on any API surface is called.
const (
	OpGetSourceSnapshotAssignment Operation = "runner_get_source_snapshot_assignment"
	OpOpenArchiveUpload           Operation = "runner_open_source_snapshot_archive_upload"
	OpReportSourceSnapshotFetch   Operation = "runner_report_source_snapshot_fetch"
	OpGetImageBuildAssignment     Operation = "runner_get_image_build_assignment"
	OpGrantPushAccess             Operation = "runner_grant_image_build_push_access"
	OpReportImageBuildResult      Operation = "runner_report_image_build_result"
	OpCreateImageBuildLogLines    Operation = "runner_create_image_build_log_lines"
)

const contentTypeJSON = "application/json"

// Operations lists every operation runner mode calls.
var Operations = []Operation{
	OpGetSourceSnapshotAssignment,
	OpOpenArchiveUpload,
	OpReportSourceSnapshotFetch,
	OpGetImageBuildAssignment,
	OpGrantPushAccess,
	OpReportImageBuildResult,
	OpCreateImageBuildLogLines,
}

// FencedError means the API no longer accepts this attempt: a 401 (the token
// is not its run's current attempt, the attempt ended, or it expired: the
// API's NOT_CURRENT, ENDED and EXPIRED refusals, which its problem body does
// not tell apart) or a 404 (the resource is not this attempt's). Nothing the
// runner does afterwards can be accepted, so it stops without reporting.
type FencedError struct {
	Operation Operation
	Status    int
	Problem   Problem
}

// Error implements error.
func (e *FencedError) Error() string {
	reason := "the run attempt token was refused"
	if e.Status == http.StatusNotFound {
		reason = "the resource is not this run attempt's"
	}

	return string(e.Operation) + ": " + reason + " (" + strconv.Itoa(e.Status) + e.Problem.suffix() + ")"
}

// APIError is a refusal the runner cannot retry and that is not fencing.
type APIError struct {
	Operation Operation
	Status    int
	Problem   Problem
}

// Error implements error.
func (e *APIError) Error() string {
	return string(e.Operation) + ": HTTP " + strconv.Itoa(e.Status) + e.Problem.suffix()
}

// Problem is the RFC 9457 body of a refusal; every runner route answers one.
type Problem struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	TraceID string `json:"traceId"`
}

// Slug is the problem type's trailing segment, the API's stable machine token.
func (p Problem) Slug() string {
	return p.Type[strings.LastIndex(p.Type, "/")+1:]
}

func (p Problem) suffix() string {
	parts := []string{}

	if slug := p.Slug(); slug != "" {
		parts = append(parts, slug)
	}

	if p.Detail != "" {
		parts = append(parts, p.Detail)
	}

	if p.TraceID != "" {
		parts = append(parts, "trace "+p.TraceID)
	}

	if len(parts) == 0 {
		return ""
	}

	return ": " + strings.Join(parts, ", ")
}

// IsFenced reports whether err is a FencedError.
func IsFenced(err error) bool {
	var fenced *FencedError

	return errors.As(err, &fenced)
}

// RetryPolicy bounds how a transient failure is retried.
type RetryPolicy struct {
	// Attempts is the most calls made, the first included.
	Attempts int
	// Base is the first backoff; each next one doubles, up to Max.
	Base time.Duration
	Max  time.Duration
}

// DefaultRetry retries a transient failure for about a minute.
var DefaultRetry = RetryPolicy{Attempts: 6, Base: time.Second, Max: 16 * time.Second}

// Client calls the runner surface with a run attempt token.
type Client struct {
	api   api.ClientWithResponsesInterface
	retry RetryPolicy
	sleep func(ctx context.Context, d time.Duration) error
}

// NewClient returns a client for the API at baseURL that authenticates every
// call with token, the run attempt token from MUSHER_API_KEY.
func NewClient(baseURL, token string, httpClient *http.Client, retry RetryPolicy) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}

	userAgent := "musher-runner/" + buildinfo.Version

	generated, err := api.NewClientWithResponses(
		strings.TrimRight(baseURL, "/")+"/",
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("User-Agent", userAgent)

			return nil
		}),
	)
	if err != nil {
		return nil, clierrors.Errorf("runner API client for %s: %w", baseURL, err)
	}

	return &Client{api: generated, retry: retry, sleep: sleepCtx}, nil
}

// SourceSnapshotAssignment is GET .../source-snapshots/{id}/assignment.
func (c *Client) SourceSnapshotAssignment(ctx context.Context, id uuid.UUID) (*api.SourceSnapshotAssignmentResponse, error) {
	return call(ctx, c, OpGetSourceSnapshotAssignment, func(ctx context.Context) (*api.SourceSnapshotAssignmentResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerGetSourceSnapshotAssignmentWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON200, resp.HTTPResponse, resp.Body, nil
	})
}

// OpenArchiveUpload is POST .../source-snapshots/{id}:openArchiveUpload.
func (c *Client) OpenArchiveUpload(ctx context.Context, id uuid.UUID, body api.OpenArchiveUploadRequest) (*api.ArchiveUploadResponse, error) {
	return call(ctx, c, OpOpenArchiveUpload, func(ctx context.Context) (*api.ArchiveUploadResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerOpenSourceSnapshotArchiveUploadWithResponse(ctx, id, body)
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON200, resp.HTTPResponse, resp.Body, nil
	})
}

// ReportFetch is POST .../source-snapshots/{id}:reportFetch.
//
// The body is the generated union type, marshaled here: the generated
// *RequestBody alias of a oneOf drops the union's MarshalJSON and would send
// "{}".
func (c *Client) ReportFetch(ctx context.Context, id uuid.UUID, body api.RunnerReportSourceSnapshotFetchJSONBody) (*api.SourceSnapshotFetchResponse, error) {
	payload, err := body.MarshalJSON()
	if err != nil {
		return nil, clierrors.Errorf("encode %s: %w", OpReportSourceSnapshotFetch, err)
	}

	return call(ctx, c, OpReportSourceSnapshotFetch, func(ctx context.Context) (*api.SourceSnapshotFetchResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerReportSourceSnapshotFetchWithBodyWithResponse(ctx, id, contentTypeJSON, bytes.NewReader(payload))
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON200, resp.HTTPResponse, resp.Body, nil
	})
}

// ImageBuildAssignment is GET .../image-builds/{id}/assignment.
func (c *Client) ImageBuildAssignment(ctx context.Context, id uuid.UUID) (*api.ImageBuildAssignmentResponse, error) {
	return call(ctx, c, OpGetImageBuildAssignment, func(ctx context.Context) (*api.ImageBuildAssignmentResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerGetImageBuildAssignmentWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON200, resp.HTTPResponse, resp.Body, nil
	})
}

// GrantPushAccess is POST .../image-builds/{id}:grantPushAccess.
func (c *Client) GrantPushAccess(ctx context.Context, id uuid.UUID) (*api.PushAccessResponse, error) {
	return call(ctx, c, OpGrantPushAccess, func(ctx context.Context) (*api.PushAccessResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerGrantImageBuildPushAccessWithResponse(ctx, id)
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON200, resp.HTTPResponse, resp.Body, nil
	})
}

// ReportResult is POST .../image-builds/{id}:reportResult.
// The body is marshaled here, as for ReportFetch.
func (c *Client) ReportResult(ctx context.Context, id uuid.UUID, body api.RunnerReportImageBuildResultJSONBody) (*api.ImageBuildResultResponse, error) {
	payload, err := body.MarshalJSON()
	if err != nil {
		return nil, clierrors.Errorf("encode %s: %w", OpReportImageBuildResult, err)
	}

	return call(ctx, c, OpReportImageBuildResult, func(ctx context.Context) (*api.ImageBuildResultResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerReportImageBuildResultWithBodyWithResponse(ctx, id, contentTypeJSON, bytes.NewReader(payload))
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON200, resp.HTTPResponse, resp.Body, nil
	})
}

// CreateLogLines is POST .../image-builds/{id}/log-lines.
func (c *Client) CreateLogLines(ctx context.Context, id uuid.UUID, body api.CreateBuildLogLinesRequest) (*api.BuildLogLinesAcceptedResponse, error) {
	return call(ctx, c, OpCreateImageBuildLogLines, func(ctx context.Context) (*api.BuildLogLinesAcceptedResponse, *http.Response, []byte, error) {
		resp, err := c.api.RunnerCreateImageBuildLogLinesWithResponse(ctx, id, body)
		if err != nil {
			return nil, nil, nil, err //nolint:wrapcheck // classified by call
		}

		return resp.JSON202, resp.HTTPResponse, resp.Body, nil
	})
}

// call runs one operation, retrying transient failures (a network error, 408,
// 425, 429 and 5xx) with backoff while ctx allows, and classifies the rest.
func call[T any](ctx context.Context, c *Client, operation Operation, send func(ctx context.Context) (*T, *http.Response, []byte, error)) (*T, error) {
	attempts := max(c.retry.Attempts, 1)
	backoff := c.retry.Base

	var lastErr error

	for attempt := 1; ; attempt++ {
		ok, resp, body, err := send(ctx) //nolint:bodyclose // the generated client has read and closed the body

		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err() //nolint:wrapcheck // the caller tests for the deadline
			}

			lastErr = clierrors.Errorf("%s: %w", operation, err)
		case ok != nil:
			return ok, nil
		default:
			status := resp.StatusCode
			problem := decodeProblem(body)

			if status == http.StatusUnauthorized || status == http.StatusNotFound {
				return nil, &FencedError{Operation: operation, Status: status, Problem: problem}
			}

			if !retryable(status) {
				return nil, &APIError{Operation: operation, Status: status, Problem: problem}
			}

			lastErr = &APIError{Operation: operation, Status: status, Problem: problem}
		}

		if attempt >= attempts {
			return nil, lastErr
		}

		if err := c.sleep(ctx, backoff); err != nil {
			return nil, err
		}

		backoff = min(backoff*2, c.retry.Max)
	}
}

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	default:
		return status >= http.StatusInternalServerError || status < http.StatusOK
	}
}

func decodeProblem(body []byte) Problem {
	var problem Problem

	if err := json.Unmarshal(body, &problem); err != nil {
		// A body that is no problem document (a proxy's HTML) leaves it empty.
		return Problem{}
	}

	return problem
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller tests for the deadline
	case <-timer.C:
		return nil
	}
}
