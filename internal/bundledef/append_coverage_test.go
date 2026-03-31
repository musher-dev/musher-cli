package bundledef

import (
	"testing"
)

func TestHasIndentedContentAfterNoNewline(t *testing.T) {
	t.Parallel()

	got := hasIndentedContentAfter("no-newline", 0)
	if got {
		t.Error("expected false for content without newline after pos")
	}
}

func TestHasIndentedContentAfterIndented(t *testing.T) {
	t.Parallel()

	content := "assets:\n  - id: foo\n    src: bar.md\n"

	got := hasIndentedContentAfter(content, 0)
	if !got {
		t.Error("expected true for indented content after first line")
	}
}

func TestHasIndentedContentAfterNotIndented(t *testing.T) {
	t.Parallel()

	content := "assets:\nname: foo\n"

	got := hasIndentedContentAfter(content, 0)
	if got {
		t.Error("expected false for non-indented content after first line")
	}
}

func TestHasIndentedContentAfterBlankThenIndented(t *testing.T) {
	t.Parallel()

	content := "assets:\n\n  - id: foo\n"

	got := hasIndentedContentAfter(content, 0)
	if !got {
		t.Error("expected true for blank line followed by indented content")
	}
}

func TestHasIndentedContentAfterBlankThenEnd(t *testing.T) {
	t.Parallel()

	content := "assets:\n\n"

	got := hasIndentedContentAfter(content, 0)
	if got {
		t.Error("expected false for blank line followed by end of content")
	}
}

func TestHasIndentedContentAfterEndOfContent(t *testing.T) {
	t.Parallel()

	content := "assets:\n"

	got := hasIndentedContentAfter(content, 0)
	if got {
		t.Error("expected false for no content after newline")
	}
}

func TestFormatAssetYAMLBasic(t *testing.T) {
	t.Parallel()

	asset := Asset{
		ID:  "review",
		Src: "skills/review/SKILL.md",
	}

	yaml := FormatAssetYAML(asset)
	if yaml == "" {
		t.Fatal("FormatAssetYAML returned empty string")
	}
}

func TestFormatAssetYAMLWithKind(t *testing.T) {
	t.Parallel()

	asset := Asset{
		ID:   "custom",
		Src:  "custom/file.md",
		Kind: "config",
	}

	yaml := FormatAssetYAML(asset)
	if yaml == "" {
		t.Fatal("FormatAssetYAML returned empty string")
	}
}
