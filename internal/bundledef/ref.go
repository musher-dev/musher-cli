package bundledef

import (
	"errors"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// Ref represents a parsed bundle reference in the form namespace/slug or
// namespace/slug:version.
type Ref struct {
	Namespace string
	Slug      string
	Version   string
}

// ParseRef parses a "namespace/slug:version" string where the version part
// is required.
func ParseRef(raw string) (Ref, error) {
	ref, err := ParseRefOptionalVersion(raw)
	if err != nil {
		return Ref{}, err
	}

	if ref.Version == "" {
		return Ref{}, repoerrors.Errorf("ref %q requires a version (use namespace/slug:version)", raw)
	}

	return ref, nil
}

// ParseRefOptionalVersion parses "namespace/slug" or "namespace/slug:version".
// If no version segment is present, Version is returned as "".
func ParseRefOptionalVersion(raw string) (Ref, error) {
	if raw == "" {
		return Ref{}, errors.New("bundle reference is empty")
	}

	nsSlug := raw
	version := ""

	if colonIdx := strings.LastIndex(raw, ":"); colonIdx > 0 {
		nsSlug = raw[:colonIdx]
		version = raw[colonIdx+1:]

		if version == "" {
			return Ref{}, repoerrors.Errorf("empty version in ref %q (use namespace/slug or namespace/slug:version)", raw)
		}
	}

	namespace, slug, ok := strings.Cut(nsSlug, "/")
	if !ok || namespace == "" || slug == "" {
		return Ref{}, repoerrors.Errorf("ref %q must be in the format namespace/slug or namespace/slug:version", raw)
	}

	// Reject slugs that contain additional slashes.
	if strings.Contains(slug, "/") {
		return Ref{}, repoerrors.Errorf("ref %q contains too many path segments (expected namespace/slug)", raw)
	}

	return Ref{
		Namespace: namespace,
		Slug:      slug,
		Version:   version,
	}, nil
}

// ParseRefNoVersion parses a "namespace/slug" string and rejects any version
// component.
func ParseRefNoVersion(raw string) (Ref, error) {
	ref, err := ParseRefOptionalVersion(raw)
	if err != nil {
		return Ref{}, err
	}

	if ref.Version != "" {
		return Ref{}, repoerrors.Errorf("unexpected version in ref %q (use namespace/slug without a version)", raw)
	}

	return ref, nil
}

// String returns the canonical string form of the ref.
func (r Ref) String() string {
	if r.Version != "" {
		return r.Namespace + "/" + r.Slug + ":" + r.Version
	}

	return r.Namespace + "/" + r.Slug
}

// HasVersion returns true if a version is specified.
func (r Ref) HasVersion() bool {
	return r.Version != ""
}
