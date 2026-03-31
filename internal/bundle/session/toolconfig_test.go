package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeJSONConfigs(t *testing.T) {
	t.Parallel()

	a := []byte(`{"mcpServers": {"server-a": {"url": "http://a"}}}`)
	b := []byte(`{"mcpServers": {"server-b": {"url": "http://b"}}}`)

	result, err := mergeJSONConfigs([][]byte{a, b})
	if err != nil {
		t.Fatalf("mergeJSONConfigs() error = %v", err)
	}

	var parsed map[string]any
	if unmarshalErr := json.Unmarshal(result, &parsed); unmarshalErr != nil {
		t.Fatalf("unmarshal result: %v", unmarshalErr)
	}

	servers, ok := parsed["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not a map")
	}

	if _, hasA := servers["server-a"]; !hasA {
		t.Error("expected server-a in merged result")
	}

	if _, hasB := servers["server-b"]; !hasB {
		t.Error("expected server-b in merged result")
	}
}

func TestMergeJSONConfigsOverride(t *testing.T) {
	t.Parallel()

	a := []byte(`{"key": "old", "nested": {"a": 1, "b": 2}}`)
	b := []byte(`{"key": "new", "nested": {"b": 3, "c": 4}}`)

	result, err := mergeJSONConfigs([][]byte{a, b})
	if err != nil {
		t.Fatalf("mergeJSONConfigs() error = %v", err)
	}

	var parsed map[string]any
	if unmarshalErr := json.Unmarshal(result, &parsed); unmarshalErr != nil {
		t.Fatalf("unmarshal result: %v", unmarshalErr)
	}

	if parsed["key"] != "new" {
		t.Errorf("key = %v, want 'new'", parsed["key"])
	}

	nested := parsed["nested"].(map[string]any)
	if nested["a"] != float64(1) {
		t.Errorf("nested.a = %v, want 1", nested["a"])
	}

	if nested["b"] != float64(3) {
		t.Errorf("nested.b = %v, want 3 (override)", nested["b"])
	}

	if nested["c"] != float64(4) {
		t.Errorf("nested.c = %v, want 4", nested["c"])
	}
}

func TestMergeTOMLConfigs(t *testing.T) {
	t.Parallel()

	a := []byte("[section_a]\nkey = \"value_a\"\n")
	b := []byte("[section_b]\nkey = \"value_b\"\n")

	result, err := mergeTOMLConfigs([][]byte{a, b})
	if err != nil {
		t.Fatalf("mergeTOMLConfigs() error = %v", err)
	}

	expected := "[section_a]\nkey = \"value_a\"\n\n[section_b]\nkey = \"value_b\"\n"
	if string(result) != expected {
		t.Errorf("result = %q, want %q", string(result), expected)
	}
}

func TestMergeTOMLConfigsEmpty(t *testing.T) {
	t.Parallel()

	_, err := mergeTOMLConfigs(nil)
	if err == nil {
		t.Error("expected error for empty inputs")
	}
}

func TestMergeToolConfigsSingle(t *testing.T) {
	t.Parallel()

	content := []byte(`{"mcpServers": {"s": {}}}`)

	result, err := mergeToolConfigs([][]byte{content}, "json")
	if err != nil {
		t.Fatalf("mergeToolConfigs() error = %v", err)
	}

	// Single item should be returned as-is.
	if !bytes.Equal(result, content) {
		t.Errorf("result = %q, want %q", string(result), string(content))
	}
}

func TestPrepareToolConfigStandalone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := []byte(`{"mcpServers": {"test": {"url": "http://test"}}}`)

	configPath, cleanups, err := prepareToolConfig(
		"--mcp-config",
		".mcp.json",
		"json",
		[][]byte{content},
		dir,
		"/project",
	)
	if err != nil {
		t.Fatalf("prepareToolConfig() error = %v", err)
	}

	if configPath == "" {
		t.Fatal("expected non-empty configPath")
	}

	if len(cleanups) != 0 {
		t.Errorf("expected 0 cleanups for standalone mode, got %d", len(cleanups))
	}

	// Verify file was written.
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}

	if !bytes.Equal(got, content) {
		t.Errorf("file content = %q, want %q", string(got), string(content))
	}
}

func TestPrepareToolConfigMergeIntoExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existingPath := filepath.Join(dir, ".cursor", "mcp.json")

	if mkErr := os.MkdirAll(filepath.Dir(existingPath), 0o755); mkErr != nil {
		t.Fatalf("MkdirAll() error = %v", mkErr)
	}

	existing := []byte(`{"mcpServers": {"existing": {"url": "http://existing"}}}`)
	if writeErr := os.WriteFile(existingPath, existing, 0o644); writeErr != nil {
		t.Fatalf("WriteFile() error = %v", writeErr)
	}

	bundle := []byte(`{"mcpServers": {"bundle": {"url": "http://bundle"}}}`)

	configPath, cleanups, err := prepareToolConfig(
		"",
		".cursor/mcp.json",
		"json",
		[][]byte{bundle},
		"",
		dir,
	)
	if err != nil {
		t.Fatalf("prepareToolConfig() error = %v", err)
	}

	if configPath != existingPath {
		t.Errorf("configPath = %q, want %q", configPath, existingPath)
	}

	// Should have a restore cleanup.
	if len(cleanups) == 0 {
		t.Fatal("expected at least 1 cleanup for merge mode")
	}

	// Verify merged content has both servers.
	got, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}

	var parsed map[string]any
	if unmarshalErr := json.Unmarshal(got, &parsed); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}

	servers := parsed["mcpServers"].(map[string]any)
	if _, hasExisting := servers["existing"]; !hasExisting {
		t.Error("expected 'existing' server in merged config")
	}

	if _, hasBundle := servers["bundle"]; !hasBundle {
		t.Error("expected 'bundle' server in merged config")
	}

	// Cleanup should restore the original.
	for _, c := range cleanups {
		if cleanErr := c(); cleanErr != nil {
			t.Fatalf("cleanup() error = %v", cleanErr)
		}
	}

	restored, restoreErr := os.ReadFile(configPath)
	if restoreErr != nil {
		t.Fatalf("ReadFile() after restore error = %v", restoreErr)
	}

	if !bytes.Equal(restored, existing) {
		t.Errorf("restored content = %q, want %q", string(restored), string(existing))
	}
}

func TestPrepareToolConfigEmpty(t *testing.T) {
	t.Parallel()

	configPath, cleanups, err := prepareToolConfig(
		"--mcp-config",
		".mcp.json",
		"json",
		nil,
		"/tmp",
		"/project",
	)
	if err != nil {
		t.Fatalf("prepareToolConfig() error = %v", err)
	}

	if configPath != "" {
		t.Errorf("expected empty configPath, got %q", configPath)
	}

	if len(cleanups) != 0 {
		t.Errorf("expected 0 cleanups, got %d", len(cleanups))
	}
}
