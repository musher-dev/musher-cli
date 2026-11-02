package build

import (
	"context"
	"maps"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/containerd/platforms"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// DefaultPlatform is the platform images are built for unless told otherwise.
// Compute hosts are x86_64 Linux; building for an Apple Silicon workstation's
// own arm64 would produce images the platform cannot run.
const DefaultPlatform = "linux/amd64"

// Image label keys written on every build.
const (
	LabelRevision     = "org.opencontainers.image.revision"
	LabelSource       = "org.opencontainers.image.source"
	LabelCreated      = "org.opencontainers.image.created"
	LabelRecipeDigest = "dev.musher.recipe-digest"
)

// timestampLayout is the UTC timestamp used in generated tags.
const timestampLayout = "20060102150405"

// tagPattern is the OCI distribution tag grammar.
var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// RequestOptions are the caller-supplied parts of a Request; empty fields take
// their defaults.
type RequestOptions struct {
	// Tag overrides the generated tag.
	Tag string
	// Platform overrides DefaultPlatform.
	Platform string
	// Labels are merged over the generated labels.
	Labels map[string]string
	// Now overrides the clock, for tests.
	Now func() time.Time
}

// NewRequest builds a Request for recipe rooted at root, filling in the
// default tag, platform and labels.
func NewRequest(ctx context.Context, recipe Recipe, root string, opts RequestOptions) (Request, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Request{}, clierrors.Errorf("resolve source root %s: %w", root, err)
	}

	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	stamp := now().UTC()

	tag := opts.Tag
	if tag == "" {
		tag = DefaultTag(ctx, absRoot, stamp)
	}

	if !tagPattern.MatchString(tag) {
		return Request{}, clierrors.Errorf("image tag %q is invalid: use letters, digits, '_', '.' and '-' (max 128, not starting with '.' or '-')", tag)
	}

	platform := strings.TrimSpace(opts.Platform)
	if platform == "" {
		platform = DefaultPlatform
	}

	if _, err := platforms.Parse(platform); err != nil {
		return Request{}, clierrors.Errorf("platform %q is invalid (want os/arch, e.g. linux/amd64): %w", platform, err)
	}

	labels := Labels(ctx, absRoot, recipe, stamp)
	maps.Copy(labels, opts.Labels)

	return Request{
		Recipe:   recipe,
		Root:     absRoot,
		Tag:      tag,
		Platform: platform,
		Labels:   labels,
	}, nil
}

// DefaultTag returns the tag a build pushes when none is given: the short git
// commit, suffixed with -dirty.<UTC yyyymmddhhmmss> when the working tree has
// uncommitted changes, or build.<UTC yyyymmddhhmmss> outside a git checkout.
// Every dirty or non-git build gets a fresh tag, so a tag never silently names
// two different images.
func DefaultTag(ctx context.Context, root string, now time.Time) string {
	stamp := now.UTC().Format(timestampLayout)

	short, err := git(ctx, root, "rev-parse", "--short", "HEAD")
	if err != nil || short == "" {
		return "build." + stamp
	}

	status, err := git(ctx, root, "status", "--porcelain")
	if err != nil || status != "" {
		return short + "-dirty." + stamp
	}

	return short
}

// Labels returns the image labels a build writes: the OCI revision, source and
// created annotations (revision and source only inside a git checkout) and the
// recipe digest.
func Labels(ctx context.Context, root string, recipe Recipe, now time.Time) map[string]string {
	labels := map[string]string{
		LabelCreated:      now.UTC().Format(time.RFC3339),
		LabelRecipeDigest: recipe.Digest(),
	}

	if revision, err := git(ctx, root, "rev-parse", "HEAD"); err == nil && revision != "" {
		labels[LabelRevision] = revision
	}

	if remote, err := git(ctx, root, "config", "--get", "remote.origin.url"); err == nil && remote != "" {
		labels[LabelSource] = redactRemote(remote)
	}

	return labels
}

// redactRemote strips credentials from a remote URL so a token embedded in
// https://user:token@host/... never lands in an image label.
func redactRemote(remote string) string {
	parsed, err := url.Parse(remote)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		// scp-like (git@github.com:acme/web.git) or unparsable: no password to strip.
		return remote
	}

	parsed.User = nil

	return parsed.String()
}

// git runs a read-only git command in dir and returns its trimmed stdout.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: fixed read-only git subcommands; no user input in args
	cmd.Dir = dir

	out, err := cmd.Output()
	if err != nil {
		return "", clierrors.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out)), nil
}
