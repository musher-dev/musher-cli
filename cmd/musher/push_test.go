package main

import (
	"testing"

	"github.com/musher-dev/musher-cli/internal/manifest"
)

func TestAssetLogicalPathUsesSrc(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		asset manifest.Asset
		want  string
	}{
		{
			name: "no installs uses src",
			asset: manifest.Asset{
				Src:  "prompts/hello.txt",
				Kind: "prompt",
			},
			want: "prompts/hello.txt",
		},
		{
			name: "single install still uses src",
			asset: manifest.Asset{
				Src:  "prompts/hello.txt",
				Kind: "prompt",
				Installs: []manifest.Install{
					{Harness: "python", Path: "~/.local/share/prompts/hello.txt"},
				},
			},
			want: "prompts/hello.txt",
		},
		{
			name: "multiple installs still uses src",
			asset: manifest.Asset{
				Src:  "scripts/setup.sh",
				Kind: "script",
				Installs: []manifest.Install{
					{Harness: "bash", Path: "/usr/local/bin/setup.sh"},
					{Harness: "zsh", Path: "/usr/local/bin/setup.sh"},
				},
			},
			want: "scripts/setup.sh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.asset.Src
			if got != tt.want {
				t.Errorf("logical path = %q, want %q", got, tt.want)
			}
		})
	}
}
