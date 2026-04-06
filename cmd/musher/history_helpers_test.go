package main

import (
	"testing"
	"time"
)

func TestFormatAge(t *testing.T) {
	t.Parallel()

	now := time.Now()

	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{"just now", now.Add(-10 * time.Second), "just now"},
		{"minutes ago", now.Add(-5 * time.Minute), "5m ago"},
		{"hours ago", now.Add(-3 * time.Hour), "3h ago"},
		{"days ago", now.Add(-48 * time.Hour), "2d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatAge(tt.at)
			if got != tt.want {
				t.Errorf("formatAge(%v) = %q, want %q", tt.at, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()

	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		want  string
	}{
		{"incomplete", base, time.Time{}, "(incomplete)"},
		{"sub-second", base, base.Add(500 * time.Millisecond), "<1s"},
		{"seconds", base, base.Add(45 * time.Second), "45s"},
		{"minutes", base, base.Add(2*time.Minute + 30*time.Second), "2m30s"},
		{"hours", base, base.Add(2*time.Hour + 15*time.Minute), "2h15m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatDuration(tt.start, tt.end)
			if got != tt.want {
				t.Errorf("formatDuration = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{"standard hours", "24h", 24 * time.Hour, false},
		{"standard minutes", "30m", 30 * time.Minute, false},
		{"days", "7d", 7 * 24 * time.Hour, false},
		{"weeks", "2w", 14 * 24 * time.Hour, false},
		{"empty", "", 0, true},
		{"invalid", "abc", 0, true},
		{"negative days", "-1d", 0, true},
		{"unrecognized suffix", "5x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseDuration(tt.raw)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
