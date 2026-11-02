package core

import (
	"strings"
	"testing"
)

func validDockerfileRecipe() Recipe {
	return Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"}
}

func TestRecipeValidate(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", maxPathLength+1)

	tests := []struct {
		name    string
		mutate  func(r *Recipe)
		wantErr string
	}{
		{name: "valid dockerfile", mutate: func(*Recipe) {}},
		{name: "valid buildpacks", mutate: func(r *Recipe) {
			r.Dockerfile = ""
			r.Buildpacks = &Buildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base"}
		}},
		{name: "valid context and args", mutate: func(r *Recipe) {
			r.Context = "services/web"
			r.Arguments = map[string]string{"NODE_ENV": "production"}
			r.Secrets = map[string]SecretSource{"NPM_TOKEN": {Env: "NPM_TOKEN"}}
		}},
		{name: "dot context", mutate: func(r *Recipe) { r.Context = "." }},
		{name: "missing repository", mutate: func(r *Recipe) { r.Repository = "" }, wantErr: "build.repository is required"},
		{name: "repository with tag", mutate: func(r *Recipe) { r.Repository = "ghcr.io/acme/web:v1" }, wantErr: "not a valid image repository"},
		{name: "neither", mutate: func(r *Recipe) { r.Dockerfile = "" }, wantErr: "neither dockerfile nor buildpacks"},
		{name: "both", mutate: func(r *Recipe) { r.Buildpacks = &Buildpacks{BuilderImage: "x"} }, wantErr: "both dockerfile and buildpacks"},
		{name: "empty builder", mutate: func(r *Recipe) {
			r.Dockerfile = ""
			r.Buildpacks = &Buildpacks{BuilderImage: " "}
		}, wantErr: "builderImage is required"},
		{name: "bad builder", mutate: func(r *Recipe) {
			r.Dockerfile = ""
			r.Buildpacks = &Buildpacks{BuilderImage: "UPPER/case"}
		}, wantErr: "not a valid image reference"},
		{name: "dockerfile dotdot", mutate: func(r *Recipe) { r.Dockerfile = "../Dockerfile" }, wantErr: "build.dockerfile"},
		{name: "dockerfile dot", mutate: func(r *Recipe) { r.Dockerfile = "." }, wantErr: "build.dockerfile"},
		{name: "dockerfile absolute", mutate: func(r *Recipe) { r.Dockerfile = "/Dockerfile" }, wantErr: "build.dockerfile"},
		{name: "dockerfile trailing slash", mutate: func(r *Recipe) { r.Dockerfile = "docker/" }, wantErr: "build.dockerfile"},
		{name: "dockerfile backslash", mutate: func(r *Recipe) { r.Dockerfile = `docker\Dockerfile` }, wantErr: "build.dockerfile"},
		{name: "dockerfile control", mutate: func(r *Recipe) { r.Dockerfile = "Docker\tfile" }, wantErr: "build.dockerfile"},
		{name: "dockerfile too long", mutate: func(r *Recipe) { r.Dockerfile = long }, wantErr: "build.dockerfile"},
		{name: "context dotdot", mutate: func(r *Recipe) { r.Context = "a/../b" }, wantErr: "build.context"},
		{name: "context empty segment", mutate: func(r *Recipe) { r.Context = "a//b" }, wantErr: "build.context"},
		{name: "argument lowercase", mutate: func(r *Recipe) { r.Arguments = map[string]string{"node_env": "x"} }, wantErr: "build.arguments key"},
		{name: "argument too long", mutate: func(r *Recipe) {
			r.Arguments = map[string]string{strings.Repeat("A", maxEnvNameLength+1): "x"}
		}, wantErr: "build.arguments key"},
		{name: "secret bad id", mutate: func(r *Recipe) {
			r.Secrets = map[string]SecretSource{"1TOKEN": {Env: "X"}}
		}, wantErr: "build.secrets ID"},
		{name: "secret clashes with argument", mutate: func(r *Recipe) {
			r.Arguments = map[string]string{"TOKEN": "x"}
			r.Secrets = map[string]SecretSource{"TOKEN": {Env: "X"}}
		}, wantErr: "also an argument"},
		{name: "secret both sources", mutate: func(r *Recipe) {
			r.Secrets = map[string]SecretSource{"TOKEN": {Env: "X", File: "f"}}
		}, wantErr: "both env and file"},
		{name: "secret no source", mutate: func(r *Recipe) {
			r.Secrets = map[string]SecretSource{"TOKEN": {Input: "token"}}
		}, wantErr: "needs a local source"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recipe := validDockerfileRecipe()
			tt.mutate(&recipe)

			err := recipe.Validate()

			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tt.wantErr != "" && err == nil:
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			case tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr):
				t.Fatalf("Validate() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRecipeKindAndContext(t *testing.T) {
	t.Parallel()

	recipe := Recipe{Dockerfile: "Dockerfile"}
	if recipe.Kind() != KindDockerfile {
		t.Errorf("Kind() = %q, want dockerfile", recipe.Kind())
	}

	if recipe.EffectiveContext() != "." {
		t.Errorf("EffectiveContext() = %q, want .", recipe.EffectiveContext())
	}

	recipe = Recipe{Context: "app", Buildpacks: &Buildpacks{}}
	if recipe.Kind() != KindBuildpacks {
		t.Errorf("Kind() = %q, want buildpacks", recipe.Kind())
	}

	if recipe.EffectiveContext() != "app" {
		t.Errorf("EffectiveContext() = %q, want app", recipe.EffectiveContext())
	}

	if (&Recipe{}).Kind() != "" {
		t.Error("empty recipe should have no kind")
	}
}

// The digest vectors were computed independently: sha256 over the JCS form,
// which for these ASCII-only objects equals Python's
// json.dumps(obj, sort_keys=True, separators=(",", ":")).
func TestRecipeDigestVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		recipe    Recipe
		canonical string
		digest    string
	}{
		{
			name:      "minimal dockerfile, context defaulted, repository excluded",
			recipe:    Recipe{Repository: "ghcr.io/acme/web", Dockerfile: "Dockerfile"},
			canonical: `{"context":".","dockerfile":"Dockerfile"}`,
			digest:    "sha256:8a79b52a71c19751ebed050bfa597417dc4b76504c2b836e7f8325c640e2d1b5",
		},
		{
			name: "full dockerfile with secret input",
			recipe: Recipe{
				Context:    "app",
				Dockerfile: "docker/web.Dockerfile",
				Arguments:  map[string]string{"NODE_ENV": "production", "B": "x"},
				Secrets:    map[string]SecretSource{"NPM_TOKEN": {Env: "LOCAL_VAR", Input: "npmToken"}},
			},
			canonical: `{"arguments":{"B":"x","NODE_ENV":"production"},"context":"app","dockerfile":"docker/web.Dockerfile","secrets":{"NPM_TOKEN":{"input":"npmToken"}}}`,
			digest:    "sha256:ac775b3f1a78cd3e44d282286694a93ddc333c35a0d7bc865a2cd6e016ac556c",
		},
		{
			name: "buildpacks",
			recipe: Recipe{
				Buildpacks: &Buildpacks{BuilderImage: "paketobuildpacks/builder-jammy-base@sha256:" + strings.Repeat("a", 64)},
			},
			canonical: `{"buildpacks":{"builderImage":"paketobuildpacks/builder-jammy-base@sha256:` + strings.Repeat("a", 64) + `"},"context":"."}`,
			digest:    "sha256:d225fe288a8329e72fb718b33ce733f9f8f684352d78530660525877d5116a5d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := canonicalJSON(tt.recipe.effectiveObject()); got != tt.canonical {
				t.Errorf("canonical = %s\nwant        %s", got, tt.canonical)
			}

			if got := tt.recipe.Digest(); got != tt.digest {
				t.Errorf("Digest() = %s, want %s", got, tt.digest)
			}
		})
	}
}

func TestRecipeDigestIgnoresEmptyAndLocalOnlyFields(t *testing.T) {
	t.Parallel()

	base := Recipe{Dockerfile: "Dockerfile"}
	want := base.Digest()

	variants := []Recipe{
		{Dockerfile: "Dockerfile", Context: "."},
		{Dockerfile: "Dockerfile", Arguments: map[string]string{}},
		{Dockerfile: "Dockerfile", Secrets: map[string]SecretSource{}},
		{Dockerfile: "Dockerfile", Secrets: map[string]SecretSource{"TOKEN": {Env: "A"}}},
		{Dockerfile: "Dockerfile", Secrets: map[string]SecretSource{"TOKEN": {File: "secret.txt"}}},
		{Dockerfile: "Dockerfile", Repository: "docker.io/acme/other"},
	}

	for i, variant := range variants {
		if got := variant.Digest(); got != want {
			t.Errorf("variant %d digest = %s, want %s", i, got, want)
		}
	}

	changed := Recipe{Dockerfile: "Dockerfile", Arguments: map[string]string{"A": "1"}}
	if changed.Digest() == want {
		t.Error("an argument must change the digest")
	}
}

func TestCanonicalJSON(t *testing.T) {
	t.Parallel()

	got := canonicalJSON(map[string]any{
		"b": "quote\" backslash\\ nl\n tab\t cr\r bs\b ff\f nul\x00 us\x1f del\x7f é  ",
		"a": map[string]any{},
		"€": "euro",
		// U+1F600 encodes as a surrogate pair (0xD83D…) and so sorts before
		// U+FFFD in UTF-16 order, although its UTF-8 form sorts after.
		"\U0001F600": "smile",
		"�":          "replacement",
	})

	want := `{"a":{},"b":"quote\" backslash\\ nl\n tab\t cr\r bs\b ff\f nul\u0000 us\u001f del` + "\x7f" + ` é ` + " " + `","€":"euro","` + "\U0001F600" + `":"smile","` + "�" + `":"replacement"}`
	if got != want {
		t.Errorf("canonicalJSON =\n%s\nwant\n%s", got, want)
	}
}

func TestCanonicalJSONPanicsOnUnsupportedType(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("expected panic for a number")
		}
	}()

	canonicalJSON(map[string]any{"n": 1})
}
