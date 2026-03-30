package validate

import (
	"strings"
	"testing"
)

func TestAPIURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{
			name:  "valid https",
			input: "https://api.musher.dev",
			want:  "https://api.musher.dev",
		},
		{
			name:  "valid http",
			input: "http://localhost:8080",
			want:  "http://localhost:8080",
		},
		{
			name:  "https with path",
			input: "https://api.musher.dev/v1",
			want:  "https://api.musher.dev/v1",
		},
		{
			name:  "https with trailing slash",
			input: "https://api.musher.dev/",
			want:  "https://api.musher.dev/",
		},
		{
			name:  "https with query string",
			input: "https://api.musher.dev?foo=bar",
			want:  "https://api.musher.dev?foo=bar",
		},
		{
			name:  "leading and trailing whitespace",
			input: "  https://api.musher.dev  ",
			want:  "https://api.musher.dev",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: "cannot be empty",
		},
		{
			name:    "whitespace only",
			input:   "   ",
			wantErr: "cannot be empty",
		},
		{
			name:    "missing scheme",
			input:   "api.musher.dev",
			wantErr: "scheme must be http or https",
		},
		{
			name:    "ftp scheme",
			input:   "ftp://files.musher.dev",
			wantErr: "scheme must be http or https",
		},
		{
			name:    "relative path",
			input:   "/v1/api",
			wantErr: "scheme must be http or https",
		},
		{
			name:    "scheme only no host",
			input:   "https://",
			wantErr: "host is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := APIURL(tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("APIURL(%q) = %q, nil; want error containing %q", tt.input, got, tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("APIURL(%q) error = %q, want it to contain %q", tt.input, err.Error(), tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("APIURL(%q) unexpected error: %v", tt.input, err)
			}

			if got != tt.want {
				t.Errorf("APIURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
