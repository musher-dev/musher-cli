package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dir       string
		content   string
		wantError string
	}{
		{
			name: "valid skill",
			dir:  "example-skill",
			content: `---
name: example-skill
description: Use this skill when validating skill bundles and checking spec compliance.
license: Apache-2.0
compatibility: Requires a Markdown-capable agent
metadata:
  author: musher
allowed-tools: Read
---

# Example
`,
		},
		{
			name: "missing frontmatter",
			dir:  "example-skill",
			content: `# Example
`,
			wantError: "missing YAML frontmatter",
		},
		{
			name: "invalid yaml",
			dir:  "example-skill",
			content: `---
name: example-skill
description: "unterminated
---
`,
			wantError: "parse frontmatter",
		},
		{
			name: "missing name",
			dir:  "example-skill",
			content: `---
description: desc
---
`,
			wantError: "name is required",
		},
		{
			name: "missing description",
			dir:  "example-skill",
			content: `---
name: example-skill
---
`,
			wantError: "description is required",
		},
		{
			name: "invalid name chars",
			dir:  "example-skill",
			content: `---
name: Example-Skill
description: desc
---
`,
			wantError: "name must contain only lowercase letters, numbers, and single hyphens",
		},
		{
			name: "name mismatch",
			dir:  "example-skill",
			content: `---
name: different-skill
description: desc
---
`,
			wantError: "must match parent directory",
		},
		{
			name:      "description too long",
			dir:       "example-skill",
			content:   "---\nname: example-skill\ndescription: " + strings.Repeat("a", 1025) + "\n---\n",
			wantError: "description must be 1-1024 characters",
		},
		{
			name:      "compatibility too long",
			dir:       "example-skill",
			content:   "---\nname: example-skill\ndescription: desc\ncompatibility: " + strings.Repeat("a", 501) + "\n---\n",
			wantError: "compatibility must be 1-500 characters when provided",
		},
		{
			name: "license wrong type",
			dir:  "example-skill",
			content: `---
name: example-skill
description: desc
license:
  type: apache
---
`,
			wantError: "license must be a string",
		},
		{
			name: "allowed tools wrong type",
			dir:  "example-skill",
			content: `---
name: example-skill
description: desc
allowed-tools:
  - Read
---
`,
			wantError: "allowed-tools must be a string",
		},
		{
			name: "metadata wrong value type",
			dir:  "example-skill",
			content: `---
name: example-skill
description: desc
metadata:
  version: 1
---
`,
			wantError: "metadata.version must be a string",
		},
		{
			name: "unknown field",
			dir:  "example-skill",
			content: `---
name: example-skill
description: desc
extra: nope
---
`,
			wantError: "unknown frontmatter field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), tt.dir)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			path := filepath.Join(dir, "SKILL.md")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write skill: %v", err)
			}

			err := ValidateFile(path)
			if tt.wantError == "" && err != nil {
				t.Fatalf("ValidateFile() error = %v", err)
			}

			if tt.wantError != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantError)
				}
			}
		})
	}
}
