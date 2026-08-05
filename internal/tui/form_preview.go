package tui

import "strings"

// PreviewPane renders a read-only content panel beside a form, typically
// showing a live preview of whatever the form is building.
type PreviewPane struct {
	sty *styles
}

// NewPreviewPane creates a new preview pane renderer.
func NewPreviewPane(sty *styles) *PreviewPane {
	return &PreviewPane{sty: sty}
}

// RenderText renders multi-line text in a muted panel, hard-truncating any
// line that would overflow the panel's inner width so the preview never
// wraps mid-record.
func (pp *PreviewPane) RenderText(title, text string, width int) string {
	innerWidth := max(width-panelContentOffset-2, 20)
	lines := strings.Split(strings.TrimSpace(text), "\n")

	truncated := make([]string, 0, len(lines))

	for _, line := range lines {
		if len(line) > innerWidth {
			truncated = append(truncated, line[:innerWidth-1]+"…")
		} else {
			truncated = append(truncated, line)
		}
	}

	content := pp.sty.muted.Render(strings.Join(truncated, "\n"))

	return renderPanel(pp.sty, title, content, width, false)
}

// RenderContent renders pre-formatted content in a titled panel as-is.
func (pp *PreviewPane) RenderContent(title, content string, width int) string {
	return renderPanel(pp.sty, title, content, width, false)
}
