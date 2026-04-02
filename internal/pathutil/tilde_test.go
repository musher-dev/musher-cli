package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "no tilde", input: "/foo/bar", want: "/foo/bar"},
		{name: "tilde prefix", input: "~/foo", want: filepath.Join(home, "foo")},
		{name: "bare tilde", input: "~", want: home},
		{name: "relative path", input: "foo/bar", want: "foo/bar"},
		{name: "tilde not at start", input: "/foo/~/bar", want: "/foo/~/bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, gotErr := ExpandTilde(tt.input)
			if (gotErr != nil) != tt.wantErr {
				t.Errorf("ExpandTilde(%q) error = %v, wantErr %v", tt.input, gotErr, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("ExpandTilde(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
