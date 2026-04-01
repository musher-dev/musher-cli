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

func TestDiscoverAssetsNewKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dir       string
		files     map[string]string // relative path → content
		wantCount int
		wantKind  string
	}{
		{
			name:      "prompts",
			dir:       "prompts",
			files:     map[string]string{"prompts/system.md": "# prompt", "prompts/greet.txt": "hello"},
			wantCount: 2,
			wantKind:  "prompt",
		},
		{
			name:      "tools",
			dir:       "tools",
			files:     map[string]string{"tools/fetch.py": "# tool", "tools/build.sh": "#!/bin/sh"},
			wantCount: 2,
			wantKind:  "tool",
		},
		{
			name:      "configs",
			dir:       "configs",
			files:     map[string]string{"configs/settings.yaml": "key: val", "configs/env.json": "{}"},
			wantCount: 2,
			wantKind:  "config",
		},
		{
			name:      "config_alt_dir",
			dir:       "config",
			files:     map[string]string{"config/defaults.toml": "[section]"},
			wantCount: 1,
			wantKind:  "config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()

			for relPath, content := range tt.files {
				absPath := filepath.Join(dir, relPath)
				if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
					t.Fatal(err)
				}

				if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			discovered, err := DiscoverAssets(dir, nil)
			if err != nil {
				t.Fatalf("DiscoverAssets() error = %v", err)
			}

			if len(discovered) != tt.wantCount {
				t.Fatalf("got %d discovered, want %d", len(discovered), tt.wantCount)
			}

			for _, d := range discovered {
				if d.Kind != tt.wantKind {
					t.Errorf("Kind = %q for %s, want %q", d.Kind, d.Src, tt.wantKind)
				}
			}
		})
	}
}

func TestDiscoverAssetsSkillAuxiliaryFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create skills/greet/ with SKILL.md and auxiliary files.
	skillDir := filepath.Join(dir, "skills", "greet")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"SKILL.md", "examples.md", "context.txt"} {
		if err := os.WriteFile(filepath.Join(skillDir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 3 {
		t.Fatalf("got %d discovered, want 3", len(discovered))
	}

	srcMap := make(map[string]DiscoveredAsset)
	for _, d := range discovered {
		srcMap[d.Src] = d
	}

	// Primary SKILL.md.
	if d, ok := srcMap["skills/greet/SKILL.md"]; !ok {
		t.Error("missing skills/greet/SKILL.md")
	} else if d.ID != "greet" {
		t.Errorf("SKILL.md ID = %q, want %q", d.ID, "greet")
	} else if d.Kind != "skill" {
		t.Errorf("SKILL.md Kind = %q, want %q", d.Kind, "skill")
	}

	// Auxiliary examples.md.
	if d, ok := srcMap["skills/greet/examples.md"]; !ok {
		t.Error("missing skills/greet/examples.md")
	} else if d.ID != "greet-examples" {
		t.Errorf("examples.md ID = %q, want %q", d.ID, "greet-examples")
	} else if d.Kind != "skill" {
		t.Errorf("examples.md Kind = %q, want %q", d.Kind, "skill")
	}

	// Auxiliary context.txt.
	if d, ok := srcMap["skills/greet/context.txt"]; !ok {
		t.Error("missing skills/greet/context.txt")
	} else if d.ID != "greet-context" {
		t.Errorf("context.txt ID = %q, want %q", d.ID, "greet-context")
	}
}

func TestDiscoverAssetsSkillAuxRequiresMarker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create skills/orphan/ with only auxiliary files (no SKILL.md).
	orphanDir := filepath.Join(dir, "skills", "orphan")
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(orphanDir, "examples.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	discovered, err := DiscoverAssets(dir, nil)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Fatalf("got %d discovered, want 0 (auxiliary files require sibling SKILL.md)", len(discovered))
	}
}

func TestDiscoverAssetsSkillAuxFiltersTracked(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	skillDir := filepath.Join(dir, "skills", "greet")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"SKILL.md", "examples.md"} {
		if err := os.WriteFile(filepath.Join(skillDir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Both files already tracked.
	def := &Def{
		Assets: []Asset{
			{ID: "greet", Src: "skills/greet/SKILL.md", Kind: "skill"},
			{ID: "greet-examples", Src: "skills/greet/examples.md", Kind: "skill"},
		},
	}

	discovered, err := DiscoverAssets(dir, def)
	if err != nil {
		t.Fatalf("DiscoverAssets() error = %v", err)
	}

	if len(discovered) != 0 {
		t.Fatalf("got %d discovered, want 0 (all files already tracked)", len(discovered))
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
		// Auxiliary skill files get parent dir + filename stem.
		{"skills/greet/examples.md", "greet-examples"},
		{"skills/greet/context.md", "greet-context"},
		{"skills/my-cool-skill/helpers.md", "my-cool-skill-helpers"},
	}

	for _, tt := range tests {
		got := InferID(tt.src)
		if got != tt.want {
			t.Errorf("InferID(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
}
