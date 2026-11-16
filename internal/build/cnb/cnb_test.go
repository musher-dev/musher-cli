package cnb

import (
	"bytes"
	"testing"

	"github.com/musher-dev/musher-cli/internal/build/core"
)

func builderConfig(labels map[string]string) *imageConfig {
	return &imageConfig{
		Env:    []string{"PATH=/usr/bin", "CNB_USER_ID=1001", "CNB_GROUP_ID=1000"},
		Labels: labels,
	}
}

func TestRunImageOf(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		label string
		want  string
	}{
		"images":        {`{"images":[{"image":"index.docker.io/paketobuildpacks/run-jammy-base:latest"}]}`, "index.docker.io/paketobuildpacks/run-jammy-base:latest"},
		"stack (older)": {`{"stack":{"runImage":{"image":"cnbs/run:jammy"}}}`, "cnbs/run:jammy"},
	}

	for name, tc := range cases {
		got, err := runImageOf(builderConfig(map[string]string{builderMetadataLabel: tc.label}))
		if err != nil || got != tc.want {
			t.Errorf("%s: runImageOf = %q, %v", name, got, err)
		}
	}

	if _, err := runImageOf(builderConfig(nil)); err == nil {
		t.Error("an image with no builder metadata named a run image")
	}
}

func TestCNBUser(t *testing.T) {
	t.Parallel()

	uid, gid, err := cnbUser(builderConfig(nil))
	if err != nil || uid != 1001 || gid != 1000 {
		t.Fatalf("cnbUser = %d:%d, %v", uid, gid, err)
	}

	if _, _, err := cnbUser(&imageConfig{}); err == nil {
		t.Fatal("an image without CNB_USER_ID was taken for a builder")
	}
}

func TestBuildRefusesAnUnpinnedBuilder(t *testing.T) {
	t.Parallel()

	_, err := New(Options{}).Build(t.Context(), &Request{BuilderImage: "paketobuildpacks/builder-jammy-base"}, nil)
	if core.StageOf(err) != core.StageRecipe {
		t.Fatalf("err = %v (stage %s), want a recipe failure", err, core.StageOf(err))
	}
}

// TestDefinitionKeepsSecretsOut checks a secret's value never enters the
// build definition (it travels by session), while arguments do, and that
// the lifecycle runs in layout mode as the builder's CNB user.
func TestDefinitionKeepsSecretsOut(t *testing.T) {
	t.Parallel()

	req := &Request{
		BuilderImage: "index.docker.io/paketobuildpacks/builder-jammy-base@sha256:5cd813b834965d9b9daf4d94b35d22bd76f743961dd5cfa7f141804bc4cef815",
		Env:          map[string]string{"BP_NODE_VERSION": "22"},
		Secrets:      map[string][]byte{"NPM_TOKEN": []byte("very-secret-value")},
	}

	def, err := definition(t.Context(), req, builderConfig(nil), "run:latest", 1001, 1000, "linux/amd64")
	if err != nil {
		t.Fatal(err)
	}

	var all []byte
	for _, op := range def.Def {
		all = append(all, op...)
	}

	if bytes.Contains(all, []byte("very-secret-value")) {
		t.Fatal("the secret's value is in the build definition")
	}

	for _, want := range []string{"BP_NODE_VERSION", "/cnb/lifecycle/creator", "-layout", "-skip-restore", "1001:1000", "NPM_TOKEN"} {
		if !bytes.Contains(all, []byte(want)) {
			t.Errorf("the definition lacks %q", want)
		}
	}
}
