package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/harness"
)

// Bundle directory injection modes.
const (
	ModeAddDir = "add_dir"
	ModeCwd    = "cwd"
)

// CleanupFunc removes injected files. Returned errors are logged but do not
// halt the cleanup chain.
type CleanupFunc func() error

// LoadSession manages the lifecycle of materializing a bundle into a
// harness-native directory layout.
type LoadSession struct {
	// BundleDir is the directory containing materialized bundle assets.
	// For add_dir mode this is a temp directory; for cwd mode it equals WorkingDir.
	BundleDir string

	// WorkingDir is the project working directory where the harness will run.
	WorkingDir string

	// Env holds environment variables to inject into the harness process.
	Env []string

	// toolConfigPath is the path to the standalone MCP config file (if any).
	toolConfigPath string

	// mcpConfigFlag is the CLI flag name for passing MCP config (e.g. "--mcp-config").
	mcpConfigFlag string

	// agentsFlag is the CLI flag name for inline agent injection (e.g. "--agents").
	agentsFlag string

	// agentFileExt, when non-empty, replaces the file extension of agent
	// assets (e.g. ".toml" causes "reviewer.md" → "reviewer.toml").
	agentFileExt string

	// agents collects raw agent file content keyed by filename.
	// Populated when agentsFlag is set, instead of writing agents to disk.
	agents map[string][]byte

	cleanups []CleanupFunc
	once     sync.Once
}

// AgentTransformFunc adapts agent file content for a target harness.
// It receives raw file bytes and the filename, and returns transformed content.
type AgentTransformFunc func(content []byte, filename string) ([]byte, error)

// PrepareLoadSession creates a LoadSession by materializing all bundle assets
// from the cache into a harness-native layout.
// The agentTransform parameter, when non-nil, is applied to agent_spec assets
// before writing them to adapt frontmatter for the target harness.
func PrepareLoadSession(
	store *cache.Store,
	spec *harness.Spec,
	agentTransform AgentTransformFunc,
	agentFileExt string,
	manifest *cache.BundleManifest,
	projectDir string,
) (*LoadSession, error) {
	mode := spec.BundleDir.Mode
	if mode == "" {
		mode = ModeCwd
	}

	sess, baseDir, err := initSession(mode, projectDir, spec.CLI.MCPConfigFlag, spec.CLI.AgentsFlag, agentFileExt)
	if err != nil {
		return nil, err
	}

	// If the spec asks us to expose the bundle directory via an env var
	// (e.g. OPENCODE_CONFIG_DIR), inject it for the harness subprocess.
	if spec.BundleDir.EnvVar != "" {
		sess.Env = append(sess.Env, spec.BundleDir.EnvVar+"="+sess.BundleDir)
	}

	if err := materializeLayers(sess, store, spec, agentTransform, manifest, mode, baseDir, projectDir); err != nil {
		sess.Cleanup()

		return nil, err
	}

	return sess, nil
}

// initSession creates a LoadSession and determines the base directory for assets.
func initSession(mode, projectDir, mcpConfigFlag, agentsFlag, agentFileExt string) (*LoadSession, string, error) {
	sess := &LoadSession{
		WorkingDir:    projectDir,
		mcpConfigFlag: mcpConfigFlag,
		agentsFlag:    agentsFlag,
		agentFileExt:  agentFileExt,
	}

	switch mode {
	case ModeAddDir:
		tmpDir, err := os.MkdirTemp("", "musher-session-*")
		if err != nil {
			return nil, "", repoerrors.Errorf("create session directory: %w", err)
		}

		sess.BundleDir = tmpDir

		sess.addCleanup(func() error {
			return os.RemoveAll(tmpDir)
		})

		return sess, tmpDir, nil

	case ModeCwd:
		sess.BundleDir = projectDir

		return sess, projectDir, nil

	default:
		return nil, "", repoerrors.Errorf("unsupported bundle directory mode: %s", mode)
	}
}

// materializeLayers reads blobs from the cache and writes them to the target layout.
func materializeLayers(
	sess *LoadSession,
	store *cache.Store,
	spec *harness.Spec,
	agentTransform AgentTransformFunc,
	manifest *cache.BundleManifest,
	mode, baseDir, projectDir string,
) error {
	var toolContents [][]byte

	dirs := newCreatedDirs()

	for _, layer := range manifest.Layers {
		content, err := store.ReadAsset(layer)
		if err != nil {
			return repoerrors.Errorf("read asset %s: %w", layer.LogicalPath, err)
		}

		if err := materializeLayer(sess, layer.AssetType, layer.LogicalPath, spec, agentTransform, mode, baseDir, content, dirs, &toolContents); err != nil {
			return repoerrors.Errorf("materialize %s: %w", layer.LogicalPath, err)
		}
	}

	return materializeToolConfigs(sess, spec, toolContents, projectDir)
}

// materializeLayer dispatches a single layer to the appropriate target directory.
// The logicalPath preserves the bundle's directory structure (e.g. "skills/code-review/SKILL.md").
// We strip the top-level type prefix and place the remainder under the spec's asset directory.
func materializeLayer(
	sess *LoadSession,
	assetType, logicalPath string,
	spec *harness.Spec,
	agentTransform AgentTransformFunc,
	mode, baseDir string,
	content []byte,
	dirs *createdDirs,
	toolContents *[][]byte,
) error {
	switch assetType {
	case "skill", "prompt", "config":
		relPath := assetRelativePath(logicalPath)

		return sess.materializeWithCleanup(mode, baseDir, filepath.Join(spec.Assets.SkillDir, filepath.Dir(relPath)), filepath.Base(relPath), content, dirs)
	case "agent_spec":
		relPath := assetRelativePath(logicalPath)
		filename := filepath.Base(relPath)

		if agentTransform != nil {
			var err error

			content, err = agentTransform(content, filename)
			if err != nil {
				return repoerrors.Errorf("transform agent %s for %s: %w", filename, spec.Name, err)
			}
		}

		// Replace agent file extension if the harness requires a different format
		// (e.g. Codex expects .toml instead of .md).
		if sess.agentFileExt != "" {
			filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + sess.agentFileExt
		}

		// When the harness supports inline agent injection (e.g. Claude's
		// --agents flag), collect content instead of writing to disk.
		// --add-dir directories are not scanned for agents.
		if sess.agentsFlag != "" {
			if sess.agents == nil {
				sess.agents = make(map[string][]byte)
			}

			sess.agents[filename] = content

			return nil
		}

		return sess.materializeWithCleanup(mode, baseDir, filepath.Join(spec.Assets.AgentDir, filepath.Dir(relPath)), filename, content, dirs)
	case "toolset":
		*toolContents = append(*toolContents, content)
	}

	return nil
}

// assetRelativePath strips the top-level type directory prefix from a logical path.
// For example, "skills/code-review/SKILL.md" → "code-review/SKILL.md".
// If there is no directory prefix, the path is returned as-is.
func assetRelativePath(logicalPath string) string {
	parts := strings.SplitN(logicalPath, "/", 2)
	if len(parts) < 2 {
		return logicalPath
	}

	return parts[1]
}

// materializeToolConfigs handles tool config assets after all layers are processed.
func materializeToolConfigs(sess *LoadSession, spec *harness.Spec, toolContents [][]byte, projectDir string) error {
	if len(toolContents) == 0 {
		return nil
	}

	configPath, toolCleanups, err := prepareToolConfig(
		spec.CLI.MCPConfigFlag,
		spec.Assets.ToolConfigFile,
		spec.MCP.Format,
		toolContents,
		sess.sessionDir(),
		projectDir,
	)
	if err != nil {
		return repoerrors.Errorf("prepare tool config: %w", err)
	}

	sess.toolConfigPath = configPath

	for _, c := range toolCleanups {
		sess.addCleanup(c)
	}

	return nil
}

// HarnessDirArgs returns the bundle directory arguments for the harness.
// For add_dir mode, returns [flag, dir]. For cwd mode, returns nil.
func (s *LoadSession) HarnessDirArgs(flag string) []string {
	if flag == "" || s.BundleDir == s.WorkingDir {
		return nil
	}

	return []string{flag, s.BundleDir}
}

// ToolConfigArgs returns CLI arguments for MCP config injection.
// Returns [flag, path] if a standalone config was written and the harness
// supports it, otherwise nil.
func (s *LoadSession) ToolConfigArgs() []string {
	if s.mcpConfigFlag == "" || s.toolConfigPath == "" {
		return nil
	}

	return []string{s.mcpConfigFlag, s.toolConfigPath}
}

// Cleanup runs all registered cleanup functions in LIFO order.
// It is safe to call multiple times — subsequent calls are no-ops.
func (s *LoadSession) Cleanup() {
	s.once.Do(func() {
		for i := len(s.cleanups) - 1; i >= 0; i-- {
			s.cleanups[i]() //nolint:errcheck // cleanup is best-effort; errors are intentionally ignored
		}
	})
}

// addCleanup registers a cleanup function to run when the session ends.
func (s *LoadSession) addCleanup(fn CleanupFunc) {
	s.cleanups = append(s.cleanups, fn)
}

// sessionDir returns the temp session directory for add_dir mode, or empty for cwd.
func (s *LoadSession) sessionDir() string {
	if s.BundleDir != s.WorkingDir {
		return s.BundleDir
	}

	return ""
}

// materializeWithCleanup writes an asset and registers appropriate cleanup.
// For cwd mode, it backs up existing files and tracks created directories.
func (s *LoadSession) materializeWithCleanup(
	mode, baseDir, subDir, filename string,
	content []byte,
	dirs *createdDirs,
) error {
	if mode == ModeCwd {
		if err := s.registerCwdCleanup(baseDir, subDir, filename, dirs); err != nil {
			return err
		}
	}

	_, err := materializeAsset(baseDir, subDir, filename, content)

	return err
}

// registerCwdCleanup registers backup/restore and removal cleanup for cwd mode.
func (s *LoadSession) registerCwdCleanup(baseDir, subDir, filename string, dirs *createdDirs) error {
	fullDir := filepath.Join(baseDir, subDir)
	fullPath := filepath.Join(fullDir, filename)

	// If any intermediate path component is a regular file (not a directory),
	// it would block MkdirAll. Back it up so directory creation can proceed.
	if err := s.backupBlockingFiles(baseDir, subDir, dirs); err != nil {
		return err
	}

	// Back up existing file if present.
	restore, err := backupIfExists(fullPath)

	if err != nil && !errors.Is(err, errNoBackupNeeded) {
		return err
	}

	if err == nil {
		s.addCleanup(restore)
	} else {
		s.addCleanup(removeFileCleanup(fullPath))
	}

	// Track directory for cleanup (only if we created it).
	// Also treat ENOTDIR (intermediate path is a regular file) as non-existent,
	// since MkdirAll will need to create the directory.
	if _, statErr := os.Stat(fullDir); os.IsNotExist(statErr) || errors.Is(statErr, syscall.ENOTDIR) {
		if dirs.track(fullDir) {
			s.addCleanup(removeDirIfEmptyCleanup(fullDir))
		}
	}

	return nil
}

// backupBlockingFiles walks path components of subDir relative to baseDir and
// backs up any regular file that would block MkdirAll from creating the
// directory tree (e.g. a ".codex" file blocking ".codex/agents/").
func (s *LoadSession) backupBlockingFiles(baseDir, subDir string, dirs *createdDirs) error {
	parts := strings.Split(subDir, string(filepath.Separator))
	current := baseDir

	for _, part := range parts {
		current = filepath.Join(current, part)

		if err := s.backupIfBlockingFile(current, dirs); err != nil {
			return err
		}
	}

	return nil
}

// backupIfBlockingFile checks whether path is a regular file that would block
// directory creation and backs it up if so.
func (s *LoadSession) backupIfBlockingFile(path string, dirs *createdDirs) error {
	info, err := os.Lstat(path)
	if err != nil {
		return nil //nolint:nilerr // path doesn't exist yet — no conflict to resolve
	}

	if info.IsDir() {
		return nil // already a directory — no conflict
	}

	// Regular file blocking directory creation — back it up.
	restore, backupErr := backupIfExists(path)
	if backupErr != nil && !errors.Is(backupErr, errNoBackupNeeded) {
		return backupErr
	}

	if restore != nil {
		s.addCleanup(restore)
	}

	// Track this path so the directory we create gets cleaned up.
	if dirs.track(path) {
		s.addCleanup(removeDirIfEmptyCleanup(path))
	}

	return nil
}
