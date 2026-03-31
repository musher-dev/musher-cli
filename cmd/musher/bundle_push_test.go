package main

import "testing"

func TestIsVisibilityError(t *testing.T) {
	tests := []struct {
		name   string
		detail string
		want   bool
	}{
		{"private keyword", "your plan does not allow more private bundles", true},
		{"visibility keyword", "visibility limit reached for your plan", true},
		{"plan allows keyword", "plan allows 0 private bundles", true},
		{"plan limit keyword", "plan limit exceeded for private repositories", true},
		{"case insensitive", "Your Plan Allows 0 Private bundles", true},
		{"unrelated 403", "you do not have access to this namespace", false},
		{"empty detail", "", false},
		{"permission denied", "permission denied", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isVisibilityError(tt.detail); got != tt.want {
				t.Errorf("isVisibilityError(%q) = %v, want %v", tt.detail, got, tt.want)
			}
		})
	}
}

func TestReadmeFormatFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"README.md", "markdown"},
		{"docs/readme.markdown", "markdown"},
		{"README.html", "html"},
		{"readme.htm", "html"},
		{"README.txt", "plaintext"},
		{"README.rst", "plaintext"},
		{"README", "plaintext"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := readmeFormatFromPath(tt.path); got != tt.want {
				t.Errorf("readmeFormatFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
