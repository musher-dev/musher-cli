package session

import (
	"encoding/json"
	"errors"
	"path/filepath"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// prepareToolConfig handles tool config (MCP server) assets for a harness.
//
// Strategy:
//  1. If the harness has a CLI flag for MCP config (e.g. --mcp-config), write
//     a standalone config file and return its path. No merging needed.
//  2. Otherwise, merge the bundle's tool configs into the harness's existing
//     config file, backing up the original for cleanup.
func prepareToolConfig(
	mcpConfigFlag string,
	toolConfigFile string,
	mcpFormat string,
	toolContents [][]byte,
	sessionDir string,
	projectDir string,
) (configPath string, cleanups []CleanupFunc, err error) {
	if len(toolContents) == 0 {
		return "", nil, nil
	}

	merged, err := mergeToolConfigs(toolContents, mcpFormat)
	if err != nil {
		return "", nil, err
	}

	// Strategy 1: Write standalone config, pass via CLI flag.
	if mcpConfigFlag != "" {
		baseDir := sessionDir
		if baseDir == "" {
			baseDir = projectDir
		}

		path, writeErr := writeStandaloneToolConfig(baseDir, toolConfigFile, merged)
		if writeErr != nil {
			return "", nil, writeErr
		}

		return path, nil, nil
	}

	// Strategy 2: Merge into existing config file.
	configPath, err = expandTilde(toolConfigFile)
	if err != nil {
		return "", nil, err
	}

	if !filepath.IsAbs(configPath) {
		configPath = filepath.Join(projectDir, configPath)
	}

	return mergeIntoExistingConfig(configPath, merged, mcpFormat)
}

// writeStandaloneToolConfig writes the merged tool config to a file in baseDir.
func writeStandaloneToolConfig(baseDir, toolConfigFile string, content []byte) (string, error) {
	filename := filepath.Base(toolConfigFile)
	path := filepath.Join(baseDir, ".musher-mcp-"+filename)

	dir := filepath.Dir(path)

	if err := safeio.MkdirAll(dir, 0o755); err != nil {
		return "", repoerrors.Errorf("create tool config directory: %w", err)
	}

	if err := safeio.WriteFile(path, content, 0o644); err != nil {
		return "", repoerrors.Errorf("write tool config: %w", err)
	}

	return path, nil
}

// mergeIntoExistingConfig reads the existing config at configPath, merges the
// bundle content on top, writes the result, and returns cleanup to restore.
func mergeIntoExistingConfig(configPath string, bundleContent []byte, format string) (string, []CleanupFunc, error) {
	var cleanups []CleanupFunc

	// Back up existing file if present.
	existing, existsFlag, readErr := safeio.ReadFileIfExists(configPath)
	if readErr != nil {
		return "", nil, repoerrors.Errorf("read existing config: %w", readErr)
	}

	if existsFlag {
		restore, backupErr := backupIfExists(configPath)
		if backupErr != nil && !errors.Is(backupErr, errNoBackupNeeded) {
			return "", nil, backupErr
		}

		if restore != nil {
			cleanups = append(cleanups, restore)
		}

		// Merge bundle config into existing.
		merged, mergeErr := mergeIntoExisting(existing, bundleContent, format)
		if mergeErr != nil {
			return "", nil, mergeErr
		}

		bundleContent = merged
	} else {
		// No existing file — register cleanup to remove the new file.
		cleanups = append(cleanups, removeFileCleanup(configPath))
	}

	// Ensure parent directory exists.
	if err := safeio.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return "", nil, repoerrors.Errorf("create config directory: %w", err)
	}

	if err := safeio.WriteFile(configPath, bundleContent, 0o644); err != nil {
		return "", nil, repoerrors.Errorf("write merged config: %w", err)
	}

	return configPath, cleanups, nil
}

// mergeToolConfigs combines multiple tool config asset contents into a single
// config, respecting the harness's expected format.
func mergeToolConfigs(contents [][]byte, format string) ([]byte, error) {
	if len(contents) == 1 {
		return contents[0], nil
	}

	switch format {
	case "toml":
		return mergeTOMLConfigs(contents)
	default:
		return mergeJSONConfigs(contents)
	}
}

// mergeJSONConfigs deep-merges multiple JSON objects. For MCP configs, this
// merges the "mcpServers" maps so each bundle's servers are combined.
func mergeJSONConfigs(contents [][]byte) ([]byte, error) {
	merged := make(map[string]any)

	for _, c := range contents {
		var obj map[string]any

		if err := json.Unmarshal(c, &obj); err != nil {
			return nil, repoerrors.Errorf("parse JSON tool config: %w", err)
		}

		deepMerge(merged, obj)
	}

	result, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, repoerrors.Errorf("marshal merged config: %w", err)
	}

	return result, nil
}

// mergeTOMLConfigs concatenates TOML contents with newline separators.
// This is a simple approach that works because TOML tables don't conflict
// when they have different section names.
func mergeTOMLConfigs(contents [][]byte) ([]byte, error) {
	if len(contents) == 0 {
		return nil, errors.New("no TOML contents to merge")
	}

	var result []byte

	for i, c := range contents {
		if i > 0 {
			result = append(result, '\n')
		}

		result = append(result, c...)
	}

	return result, nil
}

// mergeIntoExisting layers bundleContent on top of existingContent.
func mergeIntoExisting(existingContent, bundleContent []byte, format string) ([]byte, error) {
	switch format {
	case "toml":
		return mergeTOMLConfigs([][]byte{existingContent, bundleContent})
	default:
		return mergeJSONConfigs([][]byte{existingContent, bundleContent})
	}
}

// deepMerge recursively merges src into dst. Values in src override dst,
// except when both values are maps — in which case they are merged recursively.
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal

			continue
		}

		dstMap, dstIsMap := dstVal.(map[string]any)
		srcMap, srcIsMap := srcVal.(map[string]any)

		if dstIsMap && srcIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			dst[key] = srcVal
		}
	}
}
