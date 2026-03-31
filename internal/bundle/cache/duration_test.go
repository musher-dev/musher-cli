package cache_test

import (
	"testing"
	"time"

	"github.com/musher-dev/musher-cli/internal/bundle/cache"
)

func TestParseCacheDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{input: "24h", want: 24 * time.Hour},
		{input: "1d", want: 24 * time.Hour},
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "2w", want: 2 * 7 * 24 * time.Hour},
		{input: "1d12h", want: 36 * time.Hour},
		{input: "30m", want: 30 * time.Minute},
		{input: "1d30m", want: 24*time.Hour + 30*time.Minute},
		{input: "", wantErr: true},
		{input: "invalid", wantErr: true},
		{input: "abc123", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got, err := cache.ParseCacheDuration(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}

			if got != tt.want {
				t.Errorf("ParseCacheDuration(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
