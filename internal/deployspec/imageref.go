// Package deployspec parses and validates musher.yaml, the CLI-owned intent
// document that `musher deploy` compiles into platform payloads.
package deployspec

import (
	"regexp"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// maxImageRefLength mirrors the maxLength of ComponentImageReference.
const maxImageRefLength = 512

// floatingTags are tags whose meaning changes over time.
//
// The platform no longer refuses them — component v2 COMP-SRC-002 accepts
// every tag — so this set only drives a warning. A floating tag still means a
// redeploy can silently change what runs, which is worth saying out loud.
var floatingTags = map[string]struct{}{
	"latest":      {},
	"main":        {},
	"main-stable": {},
	"master":      {},
	"stable":      {},
	"edge":        {},
	"nightly":     {},
	"dev":         {},
	"rolling":     {},
}

var (
	// imageRefRE mirrors the ComponentImageReference pattern in the published
	// schema: [host[:port]/]path[:tag][@sha256:digest].
	imageRefRE = regexp.MustCompile(
		`^(?:[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]+)?/)?` +
			`(?:[a-z0-9][a-z0-9._-]*/)*[a-z0-9][a-z0-9._-]*` +
			`(?::[A-Za-z0-9_][A-Za-z0-9._-]{0,127})?` +
			`(?:@sha256:[0-9a-f]{64})?$`)

	// repositoryRE is an image reference with neither tag nor digest: where a
	// build is pushed.
	repositoryRE = regexp.MustCompile(
		`^(?:[A-Za-z0-9][A-Za-z0-9.-]*(?::[0-9]+)?/)?` +
			`(?:[a-z0-9][a-z0-9._-]*/)*[a-z0-9][a-z0-9._-]*$`)

	// digestPin matches the immutable form: <image>@sha256:<64 lowercase hex>.
	digestPin = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
)

// IsFloatingTag reports whether tag names a rolling or mutable tag.
func IsFloatingTag(tag string) bool {
	_, found := floatingTags[strings.ToLower(tag)]

	return found
}

// IsDigestPinned reports whether ref names an immutable image digest.
func IsDigestPinned(ref string) bool {
	return digestPin.MatchString(strings.TrimSpace(ref))
}

// ImageTag returns the tag of ref, or "" when it has none. A registry port is
// never mistaken for a tag: only a colon after the last slash counts.
func ImageTag(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if at := strings.IndexByte(trimmed, '@'); at >= 0 {
		trimmed = trimmed[:at]
	}

	name := trimmed[strings.LastIndexByte(trimmed, '/')+1:]
	if colon := strings.LastIndexByte(name, ':'); colon >= 0 {
		return name[colon+1:]
	}

	return ""
}

// ValidateImageRef reports whether ref is a well-formed image reference the
// platform's schema accepts. It checks grammar only: every tag is accepted,
// floating or not. Use FloatingTagWarning for the advisory check.
func ValidateImageRef(ref string) error {
	trimmed := strings.TrimSpace(ref)

	switch {
	case trimmed == "":
		return repoerrors.Errorf("image reference is empty")
	case len(trimmed) > maxImageRefLength:
		return repoerrors.Errorf("image reference exceeds %d characters", maxImageRefLength)
	case !imageRefRE.MatchString(trimmed):
		return repoerrors.Errorf(
			"image reference %q is not valid: expected [registry/]repository[:tag][@sha256:digest] "+
				"with a lowercase repository path", trimmed)
	default:
		return nil
	}
}

// ValidateRepository reports whether repo is a push target: an image
// reference with neither tag nor digest.
func ValidateRepository(repo string) error {
	trimmed := strings.TrimSpace(repo)
	if trimmed == "" {
		return repoerrors.Errorf("repository is empty")
	}

	if !repositoryRE.MatchString(trimmed) {
		return repoerrors.Errorf(
			"repository %q is not valid: expected [registry/]repository with no tag or digest", trimmed)
	}

	return nil
}

// FloatingTagWarning returns advice when ref can change under a running
// deployment — a floating tag, or no tag and no digest — and "" otherwise.
func FloatingTagWarning(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" || IsDigestPinned(trimmed) {
		return ""
	}

	tag := ImageTag(trimmed)

	switch {
	case tag == "":
		return "image " + trimmed + " has no tag, so it means :latest and a redeploy can change what runs; " +
			"pin a version tag or a digest (@sha256:...)"
	case IsFloatingTag(tag):
		return "image " + trimmed + " uses the floating tag :" + tag +
			", so a redeploy can change what runs; pin a version tag or a digest (@sha256:...)"
	default:
		return ""
	}
}
