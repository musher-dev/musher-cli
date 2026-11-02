// Package core holds the types shared by the build engine and its drivers.
//
// It exists so internal/build can choose a driver (internal/build/buildkit or
// internal/build/buildpacks) without an import cycle: the drivers depend on
// these types, and internal/build re-exports them under its own name. Code
// outside internal/build should use the internal/build aliases.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// DefaultContext is the build context used when a recipe omits one: the
// source root itself.
const DefaultContext = "."

// maxPathLength is the longest context or dockerfile path component v2 allows
// (COMP-BUILD-002).
const maxPathLength = 512

// maxEnvNameLength is the longest argument key or secret ID (COMP-ENVVAR-004).
const maxEnvNameLength = 128

var envNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// Recipe is a build recipe. It mirrors the component v2 workload.build object
// field for field, plus the CLI-only Repository the image is pushed to and the
// CLI-only local sources of each secret.
//
// A recipe is always written down explicitly; nothing in the build engine ever
// infers one (COMP-BUILD-007).
type Recipe struct {
	// Repository is where the built image is pushed, without tag or digest
	// (e.g. ghcr.io/acme/web). CLI-only: it is never sent to the platform.
	Repository string
	// Context is the build context, relative to the source root. Empty means ".".
	Context string
	// Dockerfile is the Dockerfile path relative to Context. Exactly one of
	// Dockerfile and Buildpacks is set.
	Dockerfile string
	// Buildpacks selects a Cloud Native Buildpacks build.
	Buildpacks *Buildpacks
	// Arguments are non-sensitive build-time values keyed by name.
	Arguments map[string]string
	// Secrets are build secrets keyed by ID, each read from a local source.
	Secrets map[string]SecretSource
}

// Buildpacks is the buildpacks half of a recipe.
type Buildpacks struct {
	// BuilderImage is the builder image reference, ideally pinned to a digest.
	BuilderImage string
}

// SecretSource says where the CLI reads a build secret's value from. Exactly
// one of Env and File is set.
type SecretSource struct {
	// Env names an environment variable holding the value.
	Env string
	// File is a path, relative to the source root unless absolute, holding the value.
	File string
	// Input optionally names the component input the platform would read this
	// secret from. It is the only part of a secret that enters RecipeDigest.
	Input string
}

// Kind reports which builder a recipe selects.
type Kind string

const (
	// KindDockerfile is a BuildKit Dockerfile build.
	KindDockerfile Kind = "dockerfile"
	// KindBuildpacks is a Cloud Native Buildpacks build.
	KindBuildpacks Kind = "buildpacks"
)

// Kind returns the builder the recipe names, or "" when it names neither or both.
func (r *Recipe) Kind() Kind {
	hasDockerfile := r.Dockerfile != ""
	hasBuildpacks := r.Buildpacks != nil

	switch {
	case hasDockerfile && !hasBuildpacks:
		return KindDockerfile
	case hasBuildpacks && !hasDockerfile:
		return KindBuildpacks
	default:
		return ""
	}
}

// EffectiveContext returns Context, or "." when it is empty.
func (r *Recipe) EffectiveContext() string {
	if r.Context == "" {
		return DefaultContext
	}

	return r.Context
}

// Validate checks the recipe against the component v2 build grammar
// (COMP-BUILD-002/003) and the CLI-only fields. It does not touch the
// filesystem or the environment.
func (r *Recipe) Validate() error {
	if err := validateRepository(r.Repository); err != nil {
		return err
	}

	if err := r.validateRecipeKind(); err != nil {
		return err
	}

	if r.Context != "" && r.Context != DefaultContext && !isCanonicalRelativePath(r.Context) {
		return clierrors.Errorf("build.context %q must be \".\" or a relative path without \"..\" segments", r.Context)
	}

	if err := validateArguments(r.Arguments); err != nil {
		return err
	}

	return r.validateSecrets()
}

func validateRepository(repository string) error {
	if repository == "" {
		return clierrors.Errorf("build.repository is required: name the image repository to push to (e.g. ghcr.io/acme/web)")
	}

	if _, err := name.NewRepository(repository); err != nil {
		return clierrors.Errorf("build.repository %q is not a valid image repository (omit any tag or digest): %w", repository, err)
	}

	return nil
}

func (r *Recipe) validateRecipeKind() error {
	switch r.Kind() {
	case KindDockerfile:
		if !isCanonicalRelativePath(r.Dockerfile) {
			return clierrors.Errorf("build.dockerfile %q must be a relative path inside the context", r.Dockerfile)
		}
	case KindBuildpacks:
		if strings.TrimSpace(r.Buildpacks.BuilderImage) == "" {
			return clierrors.Errorf("build.buildpacks.builderImage is required")
		}

		if _, err := name.ParseReference(r.Buildpacks.BuilderImage); err != nil {
			return clierrors.Errorf("build.buildpacks.builderImage %q is not a valid image reference: %w", r.Buildpacks.BuilderImage, err)
		}
	default:
		if r.Dockerfile != "" {
			return clierrors.Errorf("build names both dockerfile and buildpacks; choose exactly one")
		}

		return clierrors.Errorf("build names neither dockerfile nor buildpacks; choose exactly one")
	}

	return nil
}

func validateArguments(arguments map[string]string) error {
	for _, key := range sortedKeys(arguments) {
		if !isEnvName(key) {
			return clierrors.Errorf("build.arguments key %q must match %s", key, envNamePattern.String())
		}
	}

	return nil
}

func (r *Recipe) validateSecrets() error {
	for _, id := range sortedKeys(r.Secrets) {
		if !isEnvName(id) {
			return clierrors.Errorf("build.secrets ID %q must match %s", id, envNamePattern.String())
		}

		if _, clash := r.Arguments[id]; clash {
			return clierrors.Errorf("build.secrets ID %q is also an argument; a buildpacks build receives both through one environment", id)
		}

		src := r.Secrets[id]

		switch {
		case src.Env != "" && src.File != "":
			return clierrors.Errorf("build.secrets.%s sets both env and file; choose one", id)
		case src.Env == "" && src.File == "":
			return clierrors.Errorf("build.secrets.%s needs a local source: env or file", id)
		}
	}

	return nil
}

func isEnvName(s string) bool {
	return len(s) <= maxEnvNameLength && envNamePattern.MatchString(s)
}

// isCanonicalRelativePath implements the component v2 path grammar: one or more
// "/"-joined segments, no leading or trailing "/", no empty or all-dot segment,
// and no backslash or control character.
func isCanonicalRelativePath(path string) bool {
	if path == "" || len(path) > maxPathLength {
		return false
	}

	for _, ch := range path {
		if ch == '\\' || ch < 0x20 || ch == 0x7f {
			return false
		}
	}

	for segment := range strings.SplitSeq(path, "/") {
		if strings.Trim(segment, ".") == "" {
			return false
		}
	}

	return true
}

// Digest returns the recipe digest the blueprint v2 resolution record names:
// "sha256:" and the hex SHA-256 of the RFC 8785 canonical JSON of the effective
// build object.
//
// The effective object has context present ("." when omitted), dockerfile or
// buildpacks.builderImage, arguments as written, and each secret reduced to its
// input name, after removing every member whose value is an empty object.
// Locally a secret is read from an env var or file rather than an input, so a
// secret with no Input contributes nothing; its local source and its value are
// never part of the digest, so rotating a token does not change it.
// Repository is CLI-only and is excluded.
func (r *Recipe) Digest() string {
	sum := sha256.Sum256([]byte(canonicalJSON(r.effectiveObject())))

	return "sha256:" + hex.EncodeToString(sum[:])
}

func (r *Recipe) effectiveObject() map[string]any {
	obj := map[string]any{"context": r.EffectiveContext()}

	if r.Dockerfile != "" {
		obj["dockerfile"] = r.Dockerfile
	}

	if r.Buildpacks != nil {
		obj["buildpacks"] = map[string]any{"builderImage": r.Buildpacks.BuilderImage}
	}

	if len(r.Arguments) > 0 {
		args := make(map[string]any, len(r.Arguments))
		for key, value := range r.Arguments {
			args[key] = value
		}

		obj["arguments"] = args
	}

	if len(r.Secrets) > 0 {
		secrets := make(map[string]any, len(r.Secrets))

		for id, src := range r.Secrets {
			secret := map[string]any{}
			if src.Input != "" {
				secret["input"] = src.Input
			}

			secrets[id] = secret
		}

		obj["secrets"] = secrets
	}

	return pruneEmpty(obj)
}

// pruneEmpty removes every member whose value is an empty object, recursively,
// so a recipe that writes `arguments: {}` digests like one that omits it.
func pruneEmpty(obj map[string]any) map[string]any {
	for key, value := range obj {
		nested, ok := value.(map[string]any)
		if !ok {
			continue
		}

		if pruned := pruneEmpty(nested); len(pruned) == 0 {
			delete(obj, key)
		}
	}

	return obj
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}
