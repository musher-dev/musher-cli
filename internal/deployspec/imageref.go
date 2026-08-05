// Package deployspec compiles local deployment intent into Musher API payloads
// and validates it before anything is submitted.
package deployspec

import (
	"regexp"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// floatingTags mirrors _FLOATING_TAGS in the platform's
// contexts/component/domain/image_ref.py.
//
// The server enforces a DENYLIST, not a semver allowlist: any tag that is not
// listed here is accepted, including ":pr-441", ":abc1234", and ":2026-04-01".
// Mirroring this as an allowlist would reject references the platform happily
// runs, which is worse than doing no client-side check at all. Keep in lockstep
// with the upstream set.
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

// digestPin matches the immutable form: <image>@sha256:<64 lowercase hex>.
var digestPin = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)

// IsFloatingTag reports whether tag names a rolling or mutable tag.
func IsFloatingTag(tag string) bool {
	_, found := floatingTags[strings.ToLower(tag)]

	return found
}

// ValidateImageRef reports whether ref is a stable pin the platform will accept.
//
// Checks run in the same order as the server so the two agree exactly:
//  1. a digest pin is always accepted;
//  2. a reference with no colon at all has no tag and is rejected;
//  3. otherwise the text after the LAST colon is the tag, and it must not float.
//
// Step 3 reproduces an upstream quirk deliberately: the server splits on the
// last colon of the whole reference, so "registry.io:5000/img" is read as tag
// "5000" and accepted even though that colon is a port. A client stricter than
// the server generates support tickets, so this mirrors the behavior rather
// than correcting it.
func ValidateImageRef(ref string) error {
	trimmed := strings.TrimSpace(ref)

	if trimmed == "" {
		return repoerrors.Errorf("image reference is empty")
	}

	if digestPin.MatchString(trimmed) {
		return nil
	}

	if !strings.Contains(trimmed, ":") {
		return repoerrors.Errorf(
			"image reference %q has no tag — pin to a specific version (e.g. :v1.2.3) or a digest (@sha256:...)",
			trimmed,
		)
	}

	tag := trimmed[strings.LastIndex(trimmed, ":")+1:]
	if IsFloatingTag(tag) {
		return repoerrors.Errorf(
			"image reference %q uses floating tag %q — a redeploy could silently change what runs; "+
				"pin to a version tag (:v1.2.3), a date tag (:2026-04-01), or a digest (@sha256:...)",
			trimmed, tag,
		)
	}

	return nil
}
