package pull_test

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
	"github.com/musher-dev/musher-cli/internal/bundle/pull"
)

func TestCountAssetsByType(t *testing.T) {
	t.Parallel()

	layers := []cache.ManifestLayer{
		{AssetType: "skill"},
		{AssetType: "skill"},
		{AssetType: "agent"},
	}

	counts := pull.CountAssetsByType(layers)

	if counts["skill"] != 2 {
		t.Errorf("skill count = %d, want 2", counts["skill"])
	}

	if counts["agent"] != 1 {
		t.Errorf("agent count = %d, want 1", counts["agent"])
	}
}

func TestCountAssetsByTypeEmpty(t *testing.T) {
	t.Parallel()

	counts := pull.CountAssetsByType(nil)
	if len(counts) != 0 {
		t.Errorf("expected empty map, got %v", counts)
	}
}

func TestFormatAssetSummaryEmpty(t *testing.T) {
	t.Parallel()

	result := pull.FormatAssetSummary(nil)
	if result != "no assets" {
		t.Errorf("FormatAssetSummary(nil) = %q, want %q", result, "no assets")
	}
}

func TestFormatAssetSummaryNonEmpty(t *testing.T) {
	t.Parallel()

	layers := []cache.ManifestLayer{
		{AssetType: "skill"},
		{AssetType: "agent"},
	}

	result := pull.FormatAssetSummary(layers)
	if result == "" || result == "no assets" {
		t.Errorf("FormatAssetSummary = %q, expected non-empty summary", result)
	}
}
