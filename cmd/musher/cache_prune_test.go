package main

import (
	"strings"
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestParsePruneDurationEmpty(t *testing.T) {
	d, err := parsePruneDuration("")
	if err != nil {
		t.Fatalf("parsePruneDuration(\"\") error = %v", err)
	}

	if d != 0 {
		t.Errorf("parsePruneDuration(\"\") = %v, want 0", d)
	}
}

func TestParsePruneDurationValid(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := parsePruneDuration(tt.input)
			if err != nil {
				t.Fatalf("parsePruneDuration(%q) error = %v", tt.input, err)
			}

			if d != tt.want {
				t.Errorf("parsePruneDuration(%q) = %v, want %v", tt.input, d, tt.want)
			}
		})
	}
}

func TestParsePruneDurationInvalid(t *testing.T) {
	_, err := parsePruneDuration("not-a-duration")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}

	if !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("error = %q, want it to contain 'Invalid'", err.Error())
	}
}

func TestFilterPruneCandidatesByNamespace(t *testing.T) {
	entries := []cache.CachedBundle{
		{Namespace: "acme", Slug: "a", Version: "1.0.0", FetchedAt: time.Now()},
		{Namespace: "bob", Slug: "b", Version: "1.0.0", FetchedAt: time.Now()},
		{Namespace: "acme", Slug: "c", Version: "2.0.0", FetchedAt: time.Now()},
	}

	matching := filterPruneCandidates(entries, "acme", 0)

	if len(matching) != 2 {
		t.Fatalf("len(matching) = %d, want 2", len(matching))
	}

	for _, m := range matching {
		if m.Namespace != "acme" {
			t.Errorf("matched namespace = %q, want %q", m.Namespace, "acme")
		}
	}
}

func TestFilterPruneCandidatesByAge(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	entries := []cache.CachedBundle{
		{Namespace: "acme", Slug: "old", Version: "1.0.0", FetchedAt: old},
		{Namespace: "acme", Slug: "new", Version: "1.0.0", FetchedAt: recent},
	}

	matching := filterPruneCandidates(entries, "", 24*time.Hour)

	if len(matching) != 1 {
		t.Fatalf("len(matching) = %d, want 1", len(matching))
	}

	if matching[0].Slug != "old" {
		t.Errorf("matched slug = %q, want %q", matching[0].Slug, "old")
	}
}

func TestFilterPruneCandidatesByNamespaceAndAge(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-1 * time.Hour)

	entries := []cache.CachedBundle{
		{Namespace: "acme", Slug: "old", Version: "1.0.0", FetchedAt: old},
		{Namespace: "acme", Slug: "new", Version: "1.0.0", FetchedAt: recent},
		{Namespace: "bob", Slug: "old", Version: "1.0.0", FetchedAt: old},
	}

	matching := filterPruneCandidates(entries, "acme", 24*time.Hour)

	if len(matching) != 1 {
		t.Fatalf("len(matching) = %d, want 1", len(matching))
	}

	if matching[0].Namespace != "acme" || matching[0].Slug != "old" {
		t.Errorf("unexpected match: %s/%s", matching[0].Namespace, matching[0].Slug)
	}
}

func TestFilterPruneCandidatesEmpty(t *testing.T) {
	matching := filterPruneCandidates(nil, "acme", 24*time.Hour)

	if len(matching) != 0 {
		t.Errorf("len(matching) = %d, want 0", len(matching))
	}
}

func TestFilterPruneCandidatesZeroFetchedAt(t *testing.T) {
	entries := []cache.CachedBundle{
		{Namespace: "acme", Slug: "unknown", Version: "1.0.0", FetchedAt: time.Time{}},
	}

	// With olderThan > 0, entries with zero FetchedAt should be included
	// (since the zero time is considered "old enough")
	matching := filterPruneCandidates(entries, "", 24*time.Hour)

	if len(matching) != 1 {
		t.Fatalf("len(matching) = %d, want 1 (zero FetchedAt should match)", len(matching))
	}
}

func TestSumBundleSize(t *testing.T) {
	bundles := []cache.CachedBundle{
		{TotalSize: 100},
		{TotalSize: 200},
		{TotalSize: 50},
	}

	total := sumBundleSize(bundles)
	if total != 350 {
		t.Errorf("sumBundleSize = %d, want 350", total)
	}
}

func TestSumBundleSizeEmpty(t *testing.T) {
	total := sumBundleSize(nil)
	if total != 0 {
		t.Errorf("sumBundleSize(nil) = %d, want 0", total)
	}
}

func TestPrintPruneDryRunDoesNotPanic(t *testing.T) {
	out := testWriter()

	matching := []cache.CachedBundle{
		{Namespace: "acme", Slug: "test", Version: "1.0.0", TotalSize: 1024, FetchedAt: time.Now()},
	}

	// Should not panic
	printPruneDryRun(out, matching, 1024)
}

func TestRunCachePruneRequiresFilter(t *testing.T) {
	out := testWriter()

	err := runCachePrune(out, "", "", false)
	if err == nil {
		t.Fatal("expected error when no filter flags given")
	}

	if !strings.Contains(err.Error(), "filter flag") {
		t.Errorf("error = %q, want it to mention filter flag", err.Error())
	}
}
