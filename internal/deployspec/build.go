package deployspec

import (
	"strings"
	"unicode"
)

// AppBuild is a local build recipe: how to turn the working tree into an
// image.
//
// Context, Dockerfile, Buildpacks, Arguments and Secrets mirror component v2
// workload.build field for field. Repository is CLI-only: it names where the
// built image is pushed. Secret sources are CLI-only too — the spec only names
// secret ids; where their values come from on this machine is local.
//
// The whole block is consumed by the CLI and never sent. The platform refuses
// git and upload sources today, so the server only ever sees the pushed image
// as source.image, pinned by digest.
type AppBuild struct {
	Repository string                    `yaml:"repository"           json:"repository"`
	Context    string                    `yaml:"context,omitempty"    json:"context,omitempty"`
	Dockerfile string                    `yaml:"dockerfile,omitempty" json:"dockerfile,omitempty"`
	Buildpacks *AppBuildpacks            `yaml:"buildpacks,omitempty" json:"buildpacks,omitempty"`
	Arguments  map[string]string         `yaml:"arguments,omitempty"  json:"arguments,omitempty"`
	Secrets    map[string]AppBuildSecret `yaml:"secrets,omitempty"    json:"secrets,omitempty"`
}

// AppBuildpacks selects a Cloud Native Buildpacks build.
type AppBuildpacks struct {
	BuilderImage string `yaml:"builderImage" json:"builderImage"`
}

// AppBuildSecret says where a build secret's value comes from on this
// machine: an environment variable or a file. Exactly one is set.
type AppBuildSecret struct {
	Env  string `yaml:"env,omitempty"  json:"env,omitempty"`
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}

// ContextDir returns the build context, defaulting to the file's directory.
func (b *AppBuild) ContextDir() string {
	if strings.TrimSpace(b.Context) == "" {
		return "."
	}

	return b.Context
}

// Validate checks the recipe on its own, outside a musher.yaml document.
func (b *AppBuild) Validate() []string {
	return b.validate("build")
}

// validate returns every problem with the recipe, each prefixed with the
// recipe's location in the document.
func (b *AppBuild) validate(prefix string) []string {
	var problems []string

	if err := ValidateRepository(b.Repository); err != nil {
		problems = append(problems, prefix+".repository: "+err.Error())
	}

	if b.Context != "" {
		if reason := relativePathProblem(b.Context, true); reason != "" {
			problems = append(problems, prefix+".context "+quoteOrEmpty(b.Context)+" "+reason)
		}
	}

	problems = append(problems, b.validateStrategy(prefix)...)
	problems = append(problems, b.validateArguments(prefix)...)
	problems = append(problems, b.validateSecrets(prefix)...)

	return problems
}

// validateStrategy enforces exactly one of dockerfile and buildpacks.
func (b *AppBuild) validateStrategy(prefix string) []string {
	hasDockerfile := strings.TrimSpace(b.Dockerfile) != ""

	switch {
	case hasDockerfile && b.Buildpacks != nil:
		return []string{prefix + " sets both dockerfile and buildpacks; choose one"}
	case !hasDockerfile && b.Buildpacks == nil:
		return []string{prefix + " needs either dockerfile or buildpacks"}
	case hasDockerfile:
		if reason := relativePathProblem(b.Dockerfile, false); reason != "" {
			return []string{prefix + ".dockerfile " + quoteOrEmpty(b.Dockerfile) + " " + reason}
		}

		return nil
	}

	if err := ValidateImageRef(b.Buildpacks.BuilderImage); err != nil {
		return []string{prefix + ".buildpacks.builderImage: " + err.Error()}
	}

	return nil
}

func (b *AppBuild) validateArguments(prefix string) []string {
	var problems []string

	for _, name := range sortedKeys(b.Arguments) {
		if !isBuildID(name) {
			problems = append(problems,
				prefix+".arguments key "+quoteOrEmpty(name)+
					" must be uppercase letters, digits, and underscores, not starting with a digit")
		}
	}

	return problems
}

func (b *AppBuild) validateSecrets(prefix string) []string {
	var problems []string

	for _, name := range sortedKeys(b.Secrets) {
		secret := b.Secrets[name]
		location := prefix + ".secrets." + name

		if !isBuildID(name) {
			problems = append(problems,
				prefix+".secrets key "+quoteOrEmpty(name)+
					" must be uppercase letters, digits, and underscores, not starting with a digit")
		}

		if _, clash := b.Arguments[name]; clash {
			problems = append(problems,
				location+" is also a build argument; a value is either an argument or a secret")
		}

		hasEnv := strings.TrimSpace(secret.Env) != ""
		hasFile := strings.TrimSpace(secret.File) != ""

		if hasEnv == hasFile {
			problems = append(problems, location+" must set exactly one of env or file")
		}
	}

	return problems
}

// isBuildID reports whether name is a valid build argument or secret id.
func isBuildID(name string) bool {
	return len(name) <= maxEnvKeyLength && envKeyRE.MatchString(name)
}

// relativePathProblem explains why path is not a safe path inside the
// repository, or returns "". allowDot admits "." itself, which names the
// repository root.
func relativePathProblem(path string, allowDot bool) string {
	switch {
	case allowDot && path == ".":
		return ""
	case strings.HasPrefix(path, "/"):
		return "must be relative to the repository, not absolute"
	case strings.Contains(path, `\`):
		return "must use forward slashes"
	case strings.IndexFunc(path, unicode.IsControl) >= 0:
		return "must not contain control characters"
	}

	for segment := range strings.SplitSeq(path, "/") {
		switch segment {
		case "..":
			return "must stay inside the repository (no ..)"
		case "", ".":
			return "must not contain empty or . segments"
		}
	}

	return ""
}
