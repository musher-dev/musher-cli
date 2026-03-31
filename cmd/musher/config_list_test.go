package main

import (
	"testing"
)

func TestFlattenMapEmpty(t *testing.T) {
	result := flattenMap("", map[string]any{})
	if len(result) != 0 {
		t.Errorf("len(result) = %d, want 0", len(result))
	}
}

func TestFlattenMapFlat(t *testing.T) {
	input := map[string]any{
		"key1": "val1",
		"key2": 42,
	}

	result := flattenMap("", input)
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	if result["key1"] != "val1" {
		t.Errorf("key1 = %v, want %q", result["key1"], "val1")
	}

	if result["key2"] != 42 {
		t.Errorf("key2 = %v, want %d", result["key2"], 42)
	}
}

func TestFlattenMapNested(t *testing.T) {
	input := map[string]any{
		"top": map[string]any{
			"nested": "value",
		},
	}

	result := flattenMap("", input)
	if result["top.nested"] != "value" {
		t.Errorf("top.nested = %v, want %q", result["top.nested"], "value")
	}
}

func TestFlattenMapDeeplyNested(t *testing.T) {
	input := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": "deep",
			},
		},
	}

	result := flattenMap("", input)
	if result["a.b.c"] != "deep" {
		t.Errorf("a.b.c = %v, want %q", result["a.b.c"], "deep")
	}
}

func TestFlattenMapWithPrefix(t *testing.T) {
	input := map[string]any{
		"key": "val",
	}

	result := flattenMap("prefix", input)
	if result["prefix.key"] != "val" {
		t.Errorf("prefix.key = %v, want %q", result["prefix.key"], "val")
	}
}

func TestFlattenMapMixed(t *testing.T) {
	input := map[string]any{
		"flat": "value",
		"nested": map[string]any{
			"inner": "deep",
		},
	}

	result := flattenMap("", input)
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}

	if result["flat"] != "value" {
		t.Errorf("flat = %v, want %q", result["flat"], "value")
	}

	if result["nested.inner"] != "deep" {
		t.Errorf("nested.inner = %v, want %q", result["nested.inner"], "deep")
	}
}
