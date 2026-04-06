package bundledef_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

func FuzzParseRefOptionalVersion(f *testing.F) {
	f.Add("acme/bundle:1.0.0")
	f.Add("acme/bundle")
	f.Add("")
	f.Add("no-slash")
	f.Add("a/b:c:d")
	f.Add("a/b/c")
	f.Add("a/b:")
	f.Add("///")
	f.Add(":version")

	f.Fuzz(func(t *testing.T, raw string) {
		ref, err := bundledef.ParseRefOptionalVersion(raw)
		if err != nil {
			return
		}

		// If parsing succeeded, the fields must be non-empty.
		if ref.Namespace == "" {
			t.Error("Namespace is empty after successful parse")
		}

		if ref.Slug == "" {
			t.Error("Slug is empty after successful parse")
		}
	})
}

func FuzzParseRef(f *testing.F) {
	f.Add("acme/bundle:1.0.0")
	f.Add("a/b:v2")
	f.Add("")
	f.Add("no-slash")
	f.Add("a/b")

	f.Fuzz(func(t *testing.T, raw string) {
		ref, err := bundledef.ParseRef(raw)
		if err != nil {
			return
		}

		if ref.Version == "" {
			t.Error("Version is empty after successful ParseRef")
		}
	})
}

func FuzzUnmarshalDef(f *testing.F) {
	f.Add([]byte(`name: test
namespace: acme
slug: test
version: 0.1.0
assets:
  - id: review
    src: skills/review/SKILL.md
`))
	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`name: [invalid`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var def bundledef.Def

		// Must not panic on any input.
		_ = yaml.Unmarshal(data, &def)
	})
}
