package tui

import (
	"strings"

	"github.com/musher-dev/musher-cli/internal/bundledef"
	"gopkg.in/yaml.v3"
)

// PreviewPane renders a read-only content panel, typically showing a live YAML preview.
type PreviewPane struct {
	sty *styles
}

// NewPreviewPane creates a new preview pane renderer.
func NewPreviewPane(sty *styles) *PreviewPane {
	return &PreviewPane{sty: sty}
}

// RenderYAML renders a bundledef.Def as a YAML preview inside a panel.
func (pp *PreviewPane) RenderYAML(def *bundledef.Def, width int) string {
	data, err := yaml.Marshal(def)
	if err != nil {
		return pp.sty.errStyle.Render("error rendering preview")
	}

	yamlStr := strings.TrimSpace(string(data))

	// Truncate long lines to fit panel width.
	innerWidth := max(width-panelContentOffset-2, 20)
	lines := strings.Split(yamlStr, "\n")

	var truncated []string

	for _, line := range lines {
		if len(line) > innerWidth {
			truncated = append(truncated, line[:innerWidth-1]+"…")
		} else {
			truncated = append(truncated, line)
		}
	}

	content := pp.sty.muted.Render(strings.Join(truncated, "\n"))

	return renderPanel(pp.sty, "Preview", content, width, false)
}

// RenderContent renders arbitrary string content in a panel.
func (pp *PreviewPane) RenderContent(title, content string, width int) string {
	return renderPanel(pp.sty, title, content, width, false)
}
