// Package detect inspects a project directory and suggests a build recipe for
// `musher init` to write down.
//
// Detection only ever proposes a recipe for a person or a file to keep; a build
// never calls it, because a build recipe must always be named explicitly
// (COMP-BUILD-007).
package detect

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"

	"github.com/musher-dev/musher-cli/internal/build/core"
	"github.com/musher-dev/musher-cli/internal/build/registry"
	clierrors "github.com/musher-dev/musher-cli/internal/errors"
)

// DefaultBuilder is the buildpacks builder suggested when a project has no
// Dockerfile. PinImage resolves it to a digest before it is written down.
const DefaultBuilder = "paketobuildpacks/builder-jammy-base"

const dockerDir = "docker"

// languageMarkers maps a marker file to the language hint it implies. Hints are
// for messaging only; they never choose a recipe.
var languageMarkers = []struct {
	file string
	hint string
}{
	{"package.json", "node"},
	{"go.mod", "go"},
	{"pyproject.toml", "python"},
	{"requirements.txt", "python"},
	{"pom.xml", "java"},
	{"build.gradle", "java"},
	{"build.gradle.kts", "java"},
	{"Gemfile", "ruby"},
	{"Cargo.toml", "rust"},
	{"composer.json", "php"},
	{"Procfile", "procfile"},
}

// Detection is what a project directory looks like to the build engine.
type Detection struct {
	// Dockerfiles are candidate Dockerfile paths relative to the root, in
	// preference order: Dockerfile, docker/Dockerfile, then *.Dockerfile in
	// the root and in docker/, each group sorted.
	Dockerfiles []string
	// LanguageHints are languages suggested by marker files, deduplicated.
	LanguageHints []string
	// Suggested is the recipe to propose: a dockerfile recipe for the first
	// candidate, or a buildpacks recipe with DefaultBuilder when there is
	// none. Repository is left empty for the caller to fill in. When several
	// candidates exist the caller should ask which one to use.
	Suggested core.Recipe
}

// Ambiguous reports whether more than one Dockerfile candidate was found.
func (d *Detection) Ambiguous() bool {
	return len(d.Dockerfiles) > 1
}

// Detect inspects root.
func Detect(root string) (Detection, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Detection{}, clierrors.Errorf("inspect project directory: %w", err)
	}

	if !info.IsDir() {
		return Detection{}, clierrors.Errorf("%s is not a directory", root)
	}

	det := Detection{
		Dockerfiles:   dockerfileCandidates(root),
		LanguageHints: languageHints(root),
	}

	if len(det.Dockerfiles) > 0 {
		det.Suggested = core.Recipe{Context: core.DefaultContext, Dockerfile: det.Dockerfiles[0]}
	} else {
		det.Suggested = core.Recipe{
			Context:    core.DefaultContext,
			Buildpacks: &core.Buildpacks{BuilderImage: DefaultBuilder},
		}
	}

	return det, nil
}

func dockerfileCandidates(root string) []string {
	var candidates []string

	for _, fixed := range []string{"Dockerfile", dockerDir + "/Dockerfile"} {
		if isFile(filepath.Join(root, filepath.FromSlash(fixed))) {
			candidates = append(candidates, fixed)
		}
	}

	for _, dir := range []string{"", dockerDir} {
		candidates = append(candidates, suffixDockerfiles(root, dir)...)
	}

	return candidates
}

// suffixDockerfiles lists *.Dockerfile files directly in root/dir, sorted.
func suffixDockerfiles(root, dir string) []string {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return nil
	}

	var found []string

	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(fileName, ".Dockerfile") || fileName == ".Dockerfile" {
			continue
		}

		if dir != "" {
			fileName = dir + "/" + fileName
		}

		found = append(found, fileName)
	}

	slices.Sort(found)

	return found
}

func languageHints(root string) []string {
	var hints []string

	for _, marker := range languageMarkers {
		if isFile(filepath.Join(root, marker.file)) && !slices.Contains(hints, marker.hint) {
			hints = append(hints, marker.hint)
		}
	}

	return hints
}

func isFile(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

// PinImage resolves an image reference to name@sha256:… by asking the registry
// which digest its tag names now. A reference already pinned to a digest is
// returned unchanged. The repository is kept as written (a tag is dropped).
func PinImage(ctx context.Context, ref string) (string, error) {
	return pinImage(ctx, ref, registry.Digest)
}

func pinImage(ctx context.Context, ref string, digestOf func(context.Context, string) (string, error)) (string, error) {
	parsed, err := name.ParseReference(ref)
	if err != nil {
		return "", clierrors.Errorf("parse image reference %q: %w", ref, err)
	}

	if _, pinned := parsed.(name.Digest); pinned {
		return ref, nil
	}

	digest, err := digestOf(ctx, ref)
	if err != nil {
		return "", err
	}

	repo := ref
	if tag, ok := parsed.(name.Tag); ok {
		repo = strings.TrimSuffix(ref, ":"+tag.TagStr())
	}

	return repo + "@" + digest, nil
}
