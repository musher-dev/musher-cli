package main

import (
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.bytes)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestTimeAgo(t *testing.T) {
	tests := []struct {
		name     string
		ts       time.Time
		contains string
	}{
		{"zero", time.Time{}, "unknown"},
		{"just now", time.Now().Add(-10 * time.Second), "just now"},
		{"minutes ago", time.Now().Add(-30 * time.Minute), "minute(s) ago"},
		{"hours ago", time.Now().Add(-3 * time.Hour), "hour(s) ago"},
		{"days ago", time.Now().Add(-72 * time.Hour), "day(s) ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timeAgo(tt.ts)
			if got == "" {
				t.Fatal("timeAgo returned empty string")
			}

			if tt.contains != "" {
				found := false

				for _, s := range []string{tt.contains} {
					if len(got) >= len(s) && containsSubstring(got, s) {
						found = true
					}
				}

				if !found {
					t.Errorf("timeAgo = %q, want it to contain %q", got, tt.contains)
				}
			}
		})
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || s != "" && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}

	return false
}
