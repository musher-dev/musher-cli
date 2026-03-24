package bundledef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverAssetsSkills(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create skills/review/SKILL.md
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("got %d discovered, want 1", len(discovered))
	}

	d := discovered[0]
	if d.Src != "skills/review/SKILL.md" {
		t.Errorf("Src = %q, want %q", d.Src, "skills/review/SKILL.md")
	}

	if d.ID != "review" {
		t.Errorf("ID = %q, want %q", d.ID, "review")
	}

	if d.Kind != "skill" {
		t.Errorf("Kind = %q, want %q", d.Kind, "skill")
	}
}

func TestDiscoverAssetsAgents(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create agents/reviewer.md (flat file)
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte("# agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create agents/deploy/AGENT.yaml (nested)
	deployDir := filepath.Join(agentsDir, "deploy")
	if err := os.MkdirAll(deployDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(deployDir, "AGENT.yaml"), []byte("agent: true"), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 2 {
		t.Fatalf("got %d discovered, want 2", len(discovered))
	}

	// Check that both agents were found.
	srcSet := make(map[string]bool)
	for _, d := range discovered {
		srcSet[d.Src] = true

		if d.Kind != "agent" {
			t.Errorf("Kind = %q for %s, want %q", d.Kind, d.Src, "agent")
		}
	}

	if !srcSet["agents/reviewer.md"] {
		t.Error("missing agents/reviewer.md")
	}

	if !srcSet["agents/deploy/AGENT.yaml"] {
		t.Error("missing agents/deploy/AGENT.yaml")
	}
}

func TestDiscoverAssetsFiltersTracked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create two skills.
	for _, name := range []string{"tracked", "untracked"} {
		skillDir := filepath.Join(dir, "skills", name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# skill"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	def := &Def{
		Assets: []Asset{
			{ID: "tracked", Src: "skills/tracked/SKILL.md", Kind: "skill"},
		},
	}

	discovered, err := DiscoverAssets(dir, def)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("got %d discovered, want 1", len(discovered))
	}

	if discovered[0].Src != "skills/untracked/SKILL.md" {
		t.Errorf("Src = %q, want %q", discovered[0].Src, "skills/untracked/SKILL.md")
	}
}

func TestDiscoverAssetsIDConflict(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create agents/review.md — its inferred ID "review" conflicts with existing.
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentsDir, "review.md"), []byte("# agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	def := &Def{
		Assets: []Asset{
			{ID: "review", Src: "skills/review/SKILL.md", Kind: "skill"},
		},
	}

	discovered, err := DiscoverAssets(dir, def)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 1 {
		t.Fatalf("got %d discovered, want 1", len(discovered))
	}

	if !discovered[0].IDConflict {
		t.Error("expected IDConflict = true")
	}
}

func TestDiscoverAssetsSkipsHidden(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create .hidden skill directory.
	hiddenDir := filepath.Join(dir, "skills", ".hidden")
	if err := os.MkdirAll(hiddenDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(hiddenDir, "SKILL.md"), []byte("# skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create hidden agent file.
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(agentsDir, ".hidden.md"), []byte("# agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Fatalf("got %d discovered, want 0 (hidden files should be skipped)", len(discovered))
	}
}

func TestDiscoverAssetsEmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Fatalf("got %d discovered, want 0", len(discovered))
	}
}

func TestDiscoverAssetsSkillsIgnoresNonMarker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create skills/review/README.md — should NOT be discovered (not SKILL.md).
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(skillDir, "README.md"), []byte("# readme"), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Fatalf("got %d discovered, want 0 (only SKILL.md should be matched)", len(discovered))
	}
}

func TestInferID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		src  string
		want string
	}{
		{"skills/review/SKILL.md", "review"},
		{"agents/reviewer.md", "reviewer"},
		{"agents/deploy/AGENT.yaml", "deploy"},
		{"skills/my-cool-skill/SKILL.md", "my-cool-skill"},
		{"agents/My Agent.md", "my-agent"},
	}

	for _, tt := range tests {
		got := InferID(tt.src)
		if got != tt.want {
			t.Errorf("InferID(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
}
