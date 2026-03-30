package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestCountAssetsByType(t *testing.T) {
	layers := []cache.ManifestLayer{
		{AssetType: "skill", LogicalPath: "skills/a.md"},
		{AssetType: "skill", LogicalPath: "skills/b.md"},
		{AssetType: "agent", LogicalPath: "agents/a.md"},
		{AssetType: "config", LogicalPath: ".mcp.json"},
	}

	counts := countAssetsByType(layers)

	if counts["skill"] != 2 {
		t.Errorf("skill count = %d, want 2", counts["skill"])
	}

	if counts["agent"] != 1 {
		t.Errorf("agent count = %d, want 1", counts["agent"])
	}

	if counts["config"] != 1 {
		t.Errorf("config count = %d, want 1", counts["config"])
	}
}

func TestCountAssetsByTypeEmpty(t *testing.T) {
	counts := countAssetsByType(nil)
	if len(counts) != 0 {
		t.Errorf("expected empty map, got %v", counts)
	}
}

func TestFormatAssetSummaryEmpty(t *testing.T) {
	result := formatAssetSummary(nil)

	// Empty layers should produce a fallback summary.
	if result == "" {
		t.Error("formatAssetSummary(nil) should not be empty")
	}

	if got := len(countAssetsByType(nil)); got != 0 {
		t.Errorf("expected no asset types, got %d", got)
	}
}

func TestFormatAssetSummaryNonEmpty(t *testing.T) {
	layers := []cache.ManifestLayer{
		{AssetType: "skill", LogicalPath: "skills/a.md"},
		{AssetType: "skill", LogicalPath: "skills/b.md"},
		{AssetType: "agent", LogicalPath: "agents/a.md"},
	}

	result := formatAssetSummary(layers)

	// Should contain total count.
	if len(result) < 12 {
		t.Errorf("summary too short: %q", result)
	}
}
