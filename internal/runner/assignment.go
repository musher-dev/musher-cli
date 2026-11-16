package runner

import (
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	clierrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/runner/api"
)

// Recipe kinds the assignment names.
const (
	RecipeDockerfile = "DOCKERFILE"
	RecipeBuildpacks = "BUILDPACKS"
)

var (
	sha256Digest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	commitSHA    = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// SnapshotAssignment is a checked source snapshot assignment.
type SnapshotAssignment struct {
	SnapshotID      uuid.UUID
	FullName        string
	CloneURL        string
	CommitSHA       string
	FetchToken      string
	FetchExpiresAt  time.Time
	MaxArchiveBytes int64
}

// BuildAssignment is a checked image build assignment.
type BuildAssignment struct {
	BuildID          uuid.UUID
	Kind             string
	Context          string
	DockerfilePath   string
	BuilderImage     string
	Arguments        map[string]string
	SecretIDs        []string
	Secrets          map[string]string
	ArchiveURL       string
	ArchiveDigest    string
	ArchiveSizeBytes int64
	CommitSHA        string
	ImageRepository  string
	ImageTag         string
	DeadlineAt       time.Time
}

// errMalformed is an answer the contract promises but the API did not keep:
// the platform's fault, so it is reported as infrastructure.
func errMalformed(what, field string) error {
	return clierrors.Errorf("the %s assignment has no %s", what, field)
}

func checkSnapshotAssignment(answer *api.SourceSnapshotAssignmentResponse) (*SnapshotAssignment, error) {
	const what = "source snapshot"

	switch {
	case answer.SnapshotId == nil:
		return nil, errMalformed(what, "snapshotId")
	case answer.Repository == nil || answer.Repository.FullName == nil || answer.Repository.CloneUrl == nil:
		return nil, errMalformed(what, "repository")
	case answer.CommitSha == nil || !commitSHA.MatchString(*answer.CommitSha):
		return nil, errMalformed(what, "full commitSha")
	case answer.FetchToken == nil || *answer.FetchToken == "":
		return nil, errMalformed(what, "fetchToken")
	case answer.MaxArchiveBytes == nil || *answer.MaxArchiveBytes <= 0:
		return nil, errMalformed(what, "maxArchiveBytes")
	}

	out := &SnapshotAssignment{
		SnapshotID:      *answer.SnapshotId,
		FullName:        *answer.Repository.FullName,
		CloneURL:        *answer.Repository.CloneUrl,
		CommitSHA:       *answer.CommitSha,
		FetchToken:      *answer.FetchToken,
		MaxArchiveBytes: int64(*answer.MaxArchiveBytes),
	}

	if answer.FetchTokenExpiresAt != nil {
		out.FetchExpiresAt = *answer.FetchTokenExpiresAt
	}

	return out, nil
}

func checkBuildAssignment(answer *api.ImageBuildAssignmentResponse) (*BuildAssignment, error) {
	const what = "image build"

	switch {
	case answer.BuildId == nil:
		return nil, errMalformed(what, "buildId")
	case answer.DeadlineAt == nil:
		return nil, errMalformed(what, "deadlineAt")
	case answer.ImageRepository == nil || *answer.ImageRepository == "":
		return nil, errMalformed(what, "imageRepository")
	case answer.ImageTag == nil || *answer.ImageTag == "":
		return nil, errMalformed(what, "imageTag")
	case answer.Recipe == nil || answer.Recipe.Kind == nil:
		return nil, errMalformed(what, "recipe")
	case answer.Source == nil || answer.Source.ArchiveUrl == nil || answer.Source.ArchiveSizeBytes == nil:
		return nil, errMalformed(what, "source archive")
	case answer.Source.ArchiveDigest == nil || !sha256Digest.MatchString(*answer.Source.ArchiveDigest):
		// The digest is mandatory: an archive is never built unverified.
		return nil, errMalformed(what, "sha256 archiveDigest")
	}

	out := &BuildAssignment{
		BuildID:          *answer.BuildId,
		Kind:             *answer.Recipe.Kind,
		Context:          deref(answer.Recipe.Context),
		DockerfilePath:   deref(answer.Recipe.DockerfilePath),
		BuilderImage:     deref(answer.Recipe.BuilderImage),
		Arguments:        derefMap(answer.Recipe.Arguments),
		Secrets:          derefMap(answer.Secrets),
		ArchiveURL:       *answer.Source.ArchiveUrl,
		ArchiveDigest:    *answer.Source.ArchiveDigest,
		ArchiveSizeBytes: int64(*answer.Source.ArchiveSizeBytes),
		CommitSHA:        deref(answer.Source.CommitSha),
		ImageRepository:  *answer.ImageRepository,
		ImageTag:         *answer.ImageTag,
		DeadlineAt:       *answer.DeadlineAt,
	}

	if answer.Recipe.SecretIds != nil {
		out.SecretIDs = *answer.Recipe.SecretIds
	}

	return out, nil
}

// checkRecipe refuses a recipe that cannot apply to any source. Every
// refusal here is the recipe's fault (RECIPE_INVALID).
func checkRecipe(b *BuildAssignment) error { //nolint:varnamelen // b is the assignment throughout
	ctx := b.Context
	if ctx == "" {
		ctx = "."
	}

	if !localPath(ctx) {
		return clierrors.Errorf("the build context %q leaves the source", b.Context)
	}

	for _, id := range b.SecretIDs {
		if _, ok := b.Secrets[id]; !ok {
			return clierrors.Errorf("the recipe's secret %s has no value in the assignment", id)
		}
	}

	switch b.Kind {
	case RecipeDockerfile:
		if b.DockerfilePath == "" || !localPath(b.DockerfilePath) {
			return clierrors.Errorf("the Dockerfile path %q is not a path inside the context", b.DockerfilePath)
		}
	case RecipeBuildpacks:
		if !strings.Contains(b.BuilderImage, "@sha256:") {
			return clierrors.Errorf("the builder image %q is not pinned by digest", b.BuilderImage)
		}
	default:
		return clierrors.Errorf("unknown recipe kind %q", b.Kind)
	}

	return nil
}

// localPath reports whether p is a relative slash path that stays inside its
// base once cleaned.
func localPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return false
	}

	clean := path.Clean(p)

	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// registryHostOf returns the host of an image repository
// (<host>/org-<org>/src-<repo>).
func registryHostOf(repository string) string {
	host, _, _ := strings.Cut(repository, "/")

	return host
}

// gitHubAPIBase is the REST API of the forge cloneURL is on.
func gitHubAPIBase(cloneURL string) string {
	u, err := url.Parse(cloneURL)
	if err != nil || u.Host == "" || u.Host == "github.com" || u.Host == "www.github.com" {
		return "https://api.github.com"
	}

	return u.Scheme + "://" + u.Host + "/api/v3"
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}

	return *p
}

func derefMap(m *map[string]string) map[string]string { //nolint:gocritic // ptrToRefParam: the generated client's optional maps are pointers
	if m == nil {
		return map[string]string{}
	}

	return *m
}
