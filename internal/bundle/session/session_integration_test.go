package session_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/session"
	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/harness"
)

func setupCachedBundle(t *testing.T) (*cache.Store, *cache.BundleManifest) {
	t.Helper()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	skillContent := "---\nname: hello\ndescription: A test skill.\n---\n\n# Hello\n"
	agentContent := "# Review Agent\n\nReviews code.\n"

	// Store blobs manually via CacheBundle-like logic.
	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
		Name:      "Test",
	}

	for _, asset := range []struct {
		path    string
		atype   string
		content string
	}{
		{"skills/hello/SKILL.md", "skill", skillContent},
		{"agents/reviewer.md", "agent_spec", agentContent},
	} {
		data := []byte(asset.content)

		digest, err := store.StoreBlob(data)
		if err != nil {
			t.Fatalf("StoreBlob: %v", err)
		}

		manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
			LogicalPath:   asset.path,
			AssetType:     asset.atype,
			ContentSHA256: digest,
			Size:          int64(len(data)),
		})
	}

	return store, manifest
}

func TestPrepareLoadSession_AddDirMode(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:        "test-harness",
		DisplayName: "Test",
		Binary:      "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
		Assets: harness.AssetPaths{
			SkillDir: "skills",
			AgentDir: "agents",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	defer sess.Cleanup()

	// BundleDir should be a temp directory, not the project dir.
	if sess.BundleDir == projectDir {
		t.Error("BundleDir should differ from projectDir in add_dir mode")
	}

	// Skill should be materialized.
	skillPath := filepath.Join(sess.BundleDir, "skills", "hello", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not materialized: %v", err)
	}

	// Agent should be materialized.
	agentPath := filepath.Join(sess.BundleDir, "agents", "reviewer.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("agent not materialized: %v", err)
	}

	// HarnessDirArgs should return the flag and dir.
	args := sess.HarnessDirArgs("--add-dir")
	if len(args) != 2 || args[0] != "--add-dir" {
		t.Errorf("HarnessDirArgs = %v, want [--add-dir, dir]", args)
	}
}

func TestPrepareLoadSession_CwdMode(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:        "test-harness",
		DisplayName: "Test",
		Binary:      "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "cwd",
		},
		Assets: harness.AssetPaths{
			SkillDir: ".test/skills",
			AgentDir: ".test/agents",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	defer sess.Cleanup()

	// BundleDir should equal projectDir in cwd mode.
	if sess.BundleDir != projectDir {
		t.Error("BundleDir should equal projectDir in cwd mode")
	}

	// Skill should be in the project dir.
	skillPath := filepath.Join(projectDir, ".test", "skills", "hello", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not materialized: %v", err)
	}

	// HarnessDirArgs with no flag should return nil.
	args := sess.HarnessDirArgs("")
	if args != nil {
		t.Errorf("HarnessDirArgs = %v, want nil for cwd mode", args)
	}
}

func TestPrepareLoadSession_WithAgentTransform(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:   "test-harness",
		Binary: "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
		Assets: harness.AssetPaths{
			SkillDir: "skills",
			AgentDir: "agents",
		},
	}

	transform := func(content []byte, filename string) ([]byte, error) {
		return append([]byte("TRANSFORMED: "), content...), nil
	}

	sess, err := session.PrepareLoadSession(store, spec, transform, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	defer sess.Cleanup()

	// Agent should have transformed content.
	agentPath := filepath.Join(sess.BundleDir, "agents", "reviewer.md")

	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read agent: %v", err)
	}

	if string(data[:14]) != "TRANSFORMED: #" {
		t.Errorf("agent content = %q, expected transformed prefix", string(data[:20]))
	}
}

func TestPrepareLoadSession_WithAgentFileExt(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:   "test-harness",
		Binary: "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
		Assets: harness.AssetPaths{
			SkillDir: "skills",
			AgentDir: "agents",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, ".toml", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	defer sess.Cleanup()

	// Agent should be renamed with .toml extension.
	tomlPath := filepath.Join(sess.BundleDir, "agents", "reviewer.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Errorf("agent not renamed to .toml: %v", err)
	}

	// Original .md should not exist.
	mdPath := filepath.Join(sess.BundleDir, "agents", "reviewer.md")
	if _, err := os.Stat(mdPath); !os.IsNotExist(err) {
		t.Error("original .md agent should not exist when agentFileExt is set")
	}
}

func TestPrepareLoadSession_WithInlineAgents(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:   "test-harness",
		Binary: "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
		Assets: harness.AssetPaths{
			SkillDir: "skills",
			AgentDir: "agents",
		},
		CLI: harness.CLISpec{
			AgentsFlag: "--agents",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	defer sess.Cleanup()

	// When agentsFlag is set, agents should NOT be written to disk.
	agentPath := filepath.Join(sess.BundleDir, "agents", "reviewer.md")
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Error("agent should not be on disk when inline agents are used")
	}

	// AgentArgs should return non-nil.
	args := sess.AgentArgs()
	if len(args) == 0 {
		t.Error("AgentArgs should return args when inline agents are available")
	}
}

func TestPrepareLoadSession_Cleanup(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:   "test-harness",
		Binary: "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
		Assets: harness.AssetPaths{
			SkillDir: "skills",
			AgentDir: "agents",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	bundleDir := sess.BundleDir

	// Verify temp dir exists.
	if _, err := os.Stat(bundleDir); err != nil {
		t.Fatalf("bundle dir not created: %v", err)
	}

	// Cleanup.
	sess.Cleanup()

	// Verify temp dir was removed.
	if _, err := os.Stat(bundleDir); !os.IsNotExist(err) {
		t.Error("bundle dir should be cleaned up")
	}

	// Double cleanup should be safe (no-op).
	sess.Cleanup()
}

func TestPrepareLoadSession_CwdModeCleanup(t *testing.T) {
	t.Parallel()

	store, manifest := setupCachedBundle(t)
	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:   "test-harness",
		Binary: "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "cwd",
		},
		Assets: harness.AssetPaths{
			SkillDir: ".test/skills",
			AgentDir: ".test/agents",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	// Verify files are in the project dir.
	skillPath := filepath.Join(projectDir, ".test", "skills", "hello", "SKILL.md")
	if _, statErr := os.Stat(skillPath); statErr != nil {
		t.Fatalf("skill not materialized: %v", statErr)
	}

	// Cleanup should remove injected files.
	sess.Cleanup()

	if _, statErr := os.Stat(skillPath); !os.IsNotExist(statErr) {
		t.Error("injected skill should be cleaned up in cwd mode")
	}
}

func TestPrepareLoadSession_WithToolConfig(t *testing.T) {
	t.Parallel()

	store, err := cache.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Create a bundle with a toolset asset.
	toolContent := `{"mcpServers": {"test": {"command": "echo"}}}`

	manifest := &cache.BundleManifest{
		Namespace: "acme",
		Slug:      "test",
		Version:   "1.0.0",
	}

	digest, err := store.StoreBlob([]byte(toolContent))
	if err != nil {
		t.Fatalf("StoreBlob: %v", err)
	}

	manifest.Layers = append(manifest.Layers, cache.ManifestLayer{
		LogicalPath:   "tools/mcp.json",
		AssetType:     "toolset",
		ContentSHA256: digest,
		Size:          int64(len(toolContent)),
	})

	projectDir := t.TempDir()

	spec := &harness.Spec{
		Name:   "test-harness",
		Binary: "test-binary",
		BundleDir: harness.BundleDirSpec{
			Mode: "add_dir",
			Flag: "--add-dir",
		},
		Assets: harness.AssetPaths{
			SkillDir:       "skills",
			AgentDir:       "agents",
			ToolConfigFile: ".mcp.json",
		},
		CLI: harness.CLISpec{
			MCPConfigFlag: "--mcp-config",
		},
		MCP: harness.MCPConfig{
			Format: "json",
		},
	}

	sess, err := session.PrepareLoadSession(store, spec, nil, "", manifest, projectDir)
	if err != nil {
		t.Fatalf("PrepareLoadSession: %v", err)
	}

	defer sess.Cleanup()

	// ToolConfigArgs should return a path.
	args := sess.ToolConfigArgs()
	if len(args) != 2 {
		t.Fatalf("ToolConfigArgs = %v, want 2 elements", args)
	}

	if args[0] != "--mcp-config" {
		t.Errorf("ToolConfigArgs[0] = %q, want %q", args[0], "--mcp-config")
	}

	// The config file should exist.
	if _, err := os.Stat(args[1]); err != nil {
		t.Errorf("tool config file not found: %v", err)
	}
}

// Unused import guard — only used for setupCachedBundle but keeping the
// import for documentation that this tests the same flow as production.
var _ = client.PullBundleResponse{}
