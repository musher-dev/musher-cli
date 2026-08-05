package client

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

const (
	// maxProblemBody bounds how much of an error body is read. A misbehaving
	// proxy can stream megabytes of HTML; the CLI only ever needs the head.
	maxProblemBody = 64 << 10

	// maxSnippetBytes bounds the non-JSON fallback snippet. Error bodies that
	// are not problem documents (proxy HTML, captive portals, plain text) are
	// truncated to this many bytes before being sanitized for terminal output.
	maxSnippetBytes = 200

	mediaProblemJSON = "application/problem+json"
	mediaJSON        = "application/json"

	// unknownErrorText is the last-resort rendering when a problem document
	// carries neither detail, title, nor a recognizable status.
	unknownErrorText = "unknown error"

	// aboutBlank is the RFC 9457 sentinel for "no specific problem type".
	aboutBlank = "about:blank"
)

// ProblemItem is one field-level error inside an RFC 9457 document's "errors"
// extension member. Pointer is a JSON Pointer into the offending request body.
type ProblemItem struct {
	Pointer string `json:"pointer,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Problem is a decoded RFC 9457 "problem details" document as returned by the
// Musher API.
//
// Extensions holds every member that is not one of the standard fields, so
// subclass data (entitlementKey, requiredTiers, revision tokens, ...) survives
// decoding without this package needing to know each error subclass.
//
//nolint:errname // RFC 9457 names this document a "problem"; the type name is part of the transport vocabulary.
type Problem struct {
	// Type is the full problem type URI, e.g.
	// https://api.platform.musher.dev/errors/entitlement-required.
	Type string

	// Title is the short, human-readable summary of the problem type.
	Title string

	// Status is the HTTP status code. It is taken from the response, falling
	// back to the "status" member of the document.
	Status int

	// Detail is the human-readable explanation specific to this occurrence.
	Detail string

	// Instance identifies the specific occurrence of the problem.
	Instance string

	// TraceID correlates the failure with server-side traces. It comes from
	// the X-Trace-Id/Traceparent response header, falling back to the body.
	TraceID string

	// Errors holds field-level validation failures when present.
	Errors []ProblemItem

	// Extensions holds all non-standard members, undecoded.
	Extensions map[string]json.RawMessage
}

// problemWire is the on-the-wire shape of the standard RFC 9457 members.
type problemWire struct {
	Type     string        `json:"type"`
	Title    string        `json:"title"`
	Status   int           `json:"status"`
	Detail   string        `json:"detail"`
	Instance string        `json:"instance"`
	TraceID  string        `json:"traceId"`
	Errors   []ProblemItem `json:"errors"`
}

// standardProblemMembers are the members represented by named fields on
// Problem. Everything else is preserved in Extensions.
var standardProblemMembers = map[string]struct{}{
	"type":     {},
	"title":    {},
	"status":   {},
	"detail":   {},
	"instance": {},
	"traceId":  {},
	"errors":   {},
}

// Slug returns the trailing path segment of the problem Type URI, which is the
// stable machine token for the error (e.g. "entitlement-required"). It returns
// "" when the document carries no usable type.
func (p *Problem) Slug() string {
	if p == nil {
		return ""
	}

	raw := strings.TrimSpace(p.Type)
	if raw == "" || raw == aboutBlank {
		return ""
	}

	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}

	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}

	if idx := strings.LastIndexByte(raw, '/'); idx >= 0 {
		return raw[idx+1:]
	}

	return raw
}

// Error renders the most human-readable text the document carries. It prefers
// detail, then title, and finally the HTTP status line. It is never empty.
func (p *Problem) Error() string {
	if p == nil {
		return unknownErrorText
	}

	if detail := strings.TrimSpace(p.Detail); detail != "" {
		return detail
	}

	if title := strings.TrimSpace(p.Title); title != "" {
		return title
	}

	if p.Status == 0 {
		return unknownErrorText
	}

	text := http.StatusText(p.Status)
	if text == "" {
		text = unknownErrorText
	}

	return strconv.Itoa(p.Status) + " " + text
}

// Message is an alias for Error that reads better at call sites that treat the
// problem as data rather than as an error value.
func (p *Problem) Message() string { return p.Error() }

// ExitCode maps the problem onto a CLI exit code.
//
// The mapping lives here, not in internal/errors, because internal/client
// imports internal/errors — the reverse direction would be an import cycle.
// cmd/ is responsible for turning the code plus Error() into a *CLIError.
func (p *Problem) ExitCode() int {
	if p == nil {
		return repoerrors.ExitGeneral
	}

	if code, ok := exitCodeForSlug(p.Slug()); ok {
		return code
	}

	return exitCodeForStatus(p.Status)
}

// exitCodeForSlug maps the stable machine token to an exit code. The slug is a
// server vocabulary, deliberately kept as a plain string rather than a Go enum.
func exitCodeForSlug(slug string) (int, bool) {
	switch slug {
	case "entitlement-required":
		return repoerrors.ExitEntitlement, true
	case "forbidden":
		return repoerrors.ExitPermission, true
	case "resource-conflict", "revision-conflict", "immutable-state",
		"management-locked", "host-has-live-workloads", "region-has-live-workloads",
		"last-sign-in-method":
		return repoerrors.ExitConflict, true
	case "validation-error", "bad-request":
		return repoerrors.ExitInvalidSpec, true
	case "rate-limit-exceeded":
		return repoerrors.ExitRateLimited, true
	case "unauthorized", "credentials-invalid", "account-locked",
		"account-pending-verification", "account-purged", "account-scheduled-for-deletion":
		return repoerrors.ExitAuth, true
	case "bad-gateway", "service-unavailable", "cors":
		return repoerrors.ExitNetwork, true
	case "not-found", "internal-error":
		return repoerrors.ExitGeneral, true
	default:
		return 0, false
	}
}

func exitCodeForStatus(status int) int {
	switch status {
	case http.StatusUnauthorized:
		return repoerrors.ExitAuth
	case http.StatusForbidden:
		return repoerrors.ExitPermission
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return repoerrors.ExitInvalidSpec
	case http.StatusConflict:
		return repoerrors.ExitConflict
	case http.StatusTooManyRequests:
		return repoerrors.ExitRateLimited
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return repoerrors.ExitTimeout
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return repoerrors.ExitNetwork
	default:
		return repoerrors.ExitGeneral
	}
}

// decodeProblem reads an error response into a Problem.
//
// The response Content-Type decides the strategy: only problem+json and plain
// json are parsed. Anything else (proxy HTML, captive portals, plain text) is
// reduced to a short sanitized snippet, because pasting a raw upstream body
// into a terminal is both unreadable and an escape-sequence hazard.
func decodeProblem(resp *http.Response) *Problem {
	if resp == nil {
		return nil
	}

	prob := &Problem{
		Status:  resp.StatusCode,
		TraceID: responseTraceID(resp),
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProblemBody))
	if err != nil || len(body) == 0 {
		return prob
	}

	if !isJSONMediaType(resp.Header.Get("Content-Type")) {
		prob.Detail = sanitizeSnippet(body)
		return prob
	}

	var wire problemWire
	if json.Unmarshal(body, &wire) != nil {
		// Malformed or truncated JSON: fall back to the same safe snippet so
		// the user still sees something actionable.
		prob.Detail = sanitizeSnippet(body)

		return prob
	}

	prob.Type = wire.Type
	prob.Title = wire.Title
	prob.Detail = wire.Detail
	prob.Instance = wire.Instance
	prob.Errors = wire.Errors

	if prob.Status == 0 {
		prob.Status = wire.Status
	}

	if prob.TraceID == "" {
		prob.TraceID = strings.TrimSpace(wire.TraceID)
	}

	prob.Extensions = extractExtensions(body)

	return prob
}

// isJSONMediaType reports whether the Content-Type is one this package will
// attempt to parse. Parameters (charset, ...) are ignored.
func isJSONMediaType(contentType string) bool {
	trimmed := strings.TrimSpace(contentType)
	if trimmed == "" {
		return false
	}

	parsed, _, err := mime.ParseMediaType(trimmed)
	if err != nil {
		return false
	}

	return parsed == mediaProblemJSON || parsed == mediaJSON
}

// extractExtensions re-unmarshals the document to preserve every member that
// has no named field on Problem.
func extractExtensions(body []byte) map[string]json.RawMessage {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return nil
	}

	var ext map[string]json.RawMessage

	for key, value := range raw {
		if _, standard := standardProblemMembers[key]; standard {
			continue
		}

		if ext == nil {
			ext = make(map[string]json.RawMessage, len(raw))
		}

		ext[key] = value
	}

	return ext
}

// sanitizeSnippet truncates a body to maxSnippetBytes and strips everything
// that is not printable, collapsing whitespace. Control characters — notably
// ANSI escape sequences from an upstream error page — must never reach a
// terminal verbatim.
func sanitizeSnippet(body []byte) string {
	if len(body) > maxSnippetBytes {
		body = body[:maxSnippetBytes]
	}

	var builder strings.Builder

	builder.Grow(len(body))

	for _, char := range string(body) {
		switch {
		case char == utf8.RuneError:
			continue
		case unicode.IsSpace(char):
			builder.WriteByte(' ')
		case unicode.IsPrint(char):
			builder.WriteRune(char)
		}
	}

	return strings.Join(strings.Fields(builder.String()), " ")
}
