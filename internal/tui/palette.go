package tui

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// PaletteDeps assembles the palette's external collaborators. Construction
// is centralized so root.go can wire everything once and the palette stays
// purely presentational. The palette is a modal overlay rendered on top of
// the home (or any other) screen, so it intentionally does NOT include
// brand/status/footer chrome — the underlying screen owns those.
type PaletteDeps struct {
	// Global is the always-available command set.
	Global []Command

	// Resume is an optional pinned "pick up where you left off" row; nil
	// when there is nothing to resume.
	Resume *ResumeTarget

	// LoadMRU returns the persisted MRU command-id list. May be nil; the
	// palette tolerates that with an empty list.
	LoadMRU func() []string

	// SaveMRU persists an updated MRU list. Errors are intentionally
	// swallowed by the palette — losing one frame of MRU history is better
	// than refusing to dispatch a command.
	SaveMRU func([]string) error

	Styles *styles
}

// ResumeTarget describes the optional row pinned to the top of the palette's
// empty-query list. Label is the human-readable text rendered after the
// "Resume: " prefix; CommandID names the command in PaletteDeps.Global that
// the row activates, so the resume row never duplicates that command's logic.
type ResumeTarget struct {
	Label     string
	CommandID string
}

// paletteScreen is the bubbletea Screen implementing the command palette
// modal. It is pushed on top of the active screen by the App-level
// dispatcher and renders as a centered floating box, composited over the
// underlying screen by the App's view layer.
type paletteScreen struct {
	deps *PaletteDeps

	input    textinput.Model
	all      []Command
	filtered []Command
	cursor   int
	mru      []string

	width  int
	height int
	styles *styles
}

// NewPalette constructs the command palette screen.
func NewPalette(deps *PaletteDeps) Screen {
	if deps == nil {
		deps = &PaletteDeps{}
	}

	if deps.Styles == nil {
		sty := newStyles(true)
		deps.Styles = &sty
	}

	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "type a command, or pick one below"
	input.CharLimit = 128
	input.SetWidth(40)
	deps.Styles.applyInputStyles(&input)

	p := &paletteScreen{
		deps:   deps,
		input:  input,
		all:    deps.Global,
		styles: deps.Styles,
	}

	if deps.LoadMRU != nil {
		p.mru = deps.LoadMRU()
	}

	p.filter("")

	return p
}

// Init implements Screen.
func (p *paletteScreen) Init() tea.Cmd {
	return p.input.Focus()
}

// Title implements Titler.
func (p *paletteScreen) Title() string { return "Command palette" }

// IsOverlay marks the palette as a modal overlay so the App's view layer
// composites it on top of the underlying screen instead of replacing it.
func (p *paletteScreen) IsOverlay() bool { return true }

// KeyMap implements KeyMapper.
func (p *paletteScreen) KeyMap() KeyMap {
	return KeyMap{
		Groups: []KeyGroup{
			{
				Title: GroupNavigation,
				Bindings: []key.Binding{
					key.NewBinding(key.WithKeys("up", "ctrl+k"), key.WithHelp("↑", "previous")),
					key.NewBinding(key.WithKeys("down", "ctrl+j"), key.WithHelp("↓", "next")),
				},
			},
			{
				Title: GroupActions,
				Bindings: []key.Binding{
					key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "run")),
					key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "quit")),
				},
			},
		},
	}
}

// Update implements Screen.
func (p *paletteScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.input.SetWidth(p.inputWidth())

		return p, nil

	case tea.BackgroundColorMsg:
		sty := newStyles(msg.IsDark())
		p.styles = &sty
		p.deps.Styles = &sty
		p.styles.applyInputStyles(&p.input)

		return p, nil

	case tea.KeyPressMsg:
		return p.handleKey(msg)
	}

	var cmd tea.Cmd

	p.input, cmd = p.input.Update(msg)

	return p, cmd
}

// inputWidth returns the textinput width matching the current panel width.
func (p *paletteScreen) inputWidth() int {
	w := p.panelInnerWidth() - 2 // prompt + cursor
	if w < 20 {
		return 20
	}

	return w
}

func (p *paletteScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch msg.String() {
	case "up", "ctrl+k":
		if p.cursor > 0 {
			p.cursor--
		}

		return p, nil

	case "down", "ctrl+j":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}

		return p, nil

	case "enter":
		cmd := p.activate()

		return p, cmd

	case "esc":
		// Root palette: esc quits.
		return p, tea.Quit
	}

	prev := p.input.Value()

	var cmd tea.Cmd

	p.input, cmd = p.input.Update(msg)

	if p.input.Value() != prev {
		p.filter(p.input.Value())
	}

	return p, cmd
}

// activate runs the currently selected command. If the command is disabled
// the activation is silently ignored — the dimmed row is left visible so the
// user understands the command exists but is currently unavailable.
func (p *paletteScreen) activate() tea.Cmd {
	if p.cursor < 0 || p.cursor >= len(p.filtered) {
		return nil
	}

	cmd := &p.filtered[p.cursor]
	if !cmd.IsEnabled() {
		return nil
	}

	p.mru = bumpMRU(p.mru, cmd.ID)

	if p.deps.SaveMRU != nil {
		if err := p.deps.SaveMRU(p.mru); err != nil {
			_ = err
		}
	}

	if cmd.Run == nil {
		return nil
	}

	return cmd.Run()
}

// filter recomputes p.filtered against the current query and resets the
// cursor. Empty query → Resume → MRU → alphabetical-by-(group,title).
// Non-empty → fuzzy subsequence match scored by token boundary bonuses and
// MRU position.
func (p *paletteScreen) filter(query string) {
	p.cursor = 0
	needle := strings.ToLower(strings.TrimSpace(query))

	if needle == "" {
		p.filtered = p.orderedDefault()

		return
	}

	type scored struct {
		idx   int
		score int
	}

	var matches []scored

	for idx := range p.all {
		score := fuzzyScore(needle, &p.all[idx])
		if score > 0 {
			if pos := mruPosition(p.mru, p.all[idx].ID); pos >= 0 {
				score += 100 - pos
			}

			matches = append(matches, scored{idx: idx, score: score})
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	out := make([]Command, len(matches))
	for i, match := range matches {
		out[i] = p.all[match.idx]
	}

	p.filtered = out
}

// orderedDefault produces the empty-query ordering: an optional Resume row
// at the very top, then every command sorted by canonical group order then
// title. MRU does NOT reorder the empty-query list — the research's
// "spatial consistency" principle is more valuable than a marginal MRU win
// when the user is browsing rather than searching, and stable positions are
// what allow muscle memory to develop. MRU still applies as a scoring boost
// inside fuzzyScore() once the user starts typing.
func (p *paletteScreen) orderedDefault() []Command {
	var out []Command

	if p.deps.Resume != nil {
		out = append(out, Command{
			ID:       "resume",
			Title:    "Resume: " + p.deps.Resume.Label,
			Subtitle: "pick up where you left off",
			Group:    CmdGroupResume,
			Run: func() tea.Cmd {
				// Wire-up: dispatch the underlying load command. We look it
				// up by ID so the Resume row never duplicates Load logic.
				for idx := range p.all {
					if p.all[idx].ID == p.deps.Resume.CommandID && p.all[idx].Run != nil {
						return p.all[idx].Run()
					}
				}

				return nil
			},
		})
	}

	rest := make([]Command, 0, len(p.all))
	rest = append(rest, p.all...)

	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].Group != rest[j].Group {
			return groupOrder(rest[i].Group) < groupOrder(rest[j].Group)
		}

		return rest[i].Title < rest[j].Title
	})

	out = append(out, rest...)

	return out
}

// groupOrder returns the canonical sort position of a command group. The
// fixed order — RESUME, USE, CREATE, MANAGE, SYSTEM — mirrors the home
// screen's section ordering so users see the same vertical hierarchy whether
// they land in the palette or the menu home.
func groupOrder(group string) int {
	switch group {
	case CmdGroupResume:
		return 0
	case CmdGroupUse:
		return 1
	case CmdGroupCreate:
		return 2
	case CmdGroupManage:
		return 3
	case CmdGroupSystem:
		return 4
	default:
		return 99
	}
}

// fuzzyScore returns a positive score when needle is a subsequence of any
// (lower-cased) token in cmd's title or keywords. Token-start matches and
// contiguous runs are weighted higher. A score of 0 means "no match".
func fuzzyScore(needle string, cmd *Command) int {
	if needle == "" {
		return 1
	}

	hay := strings.ToLower(cmd.Title + " " + strings.Join(cmd.Keywords, " ") + " " + cmd.Subtitle)

	if strings.Contains(hay, needle) {
		// Direct substring: very high score; bonus when at a word boundary.
		base := 200
		idx := strings.Index(hay, needle)

		if idx == 0 || hay[idx-1] == ' ' {
			base += 50
		}

		return base
	}

	// Subsequence match.
	needleIdx := 0
	score := 0
	prevMatched := false

	for hayIdx := 0; hayIdx < len(hay) && needleIdx < len(needle); hayIdx++ {
		if hay[hayIdx] == needle[needleIdx] {
			score += 5
			if prevMatched {
				score += 5
			}

			if hayIdx == 0 || hay[hayIdx-1] == ' ' {
				score += 8
			}

			needleIdx++
			prevMatched = true
		} else {
			prevMatched = false
		}
	}

	if needleIdx < len(needle) {
		return 0
	}

	return score
}

// mruPosition reports the index of id in mru, or -1 if absent.
func mruPosition(mru []string, id string) int {
	for idx, entry := range mru {
		if entry == id {
			return idx
		}
	}

	return -1
}

// View implements Screen. The palette is a small centered modal box —
// the App's view layer composites it on top of the underlying screen via
// composeOverlay() so the home (or other parent screen) remains visible.
func (p *paletteScreen) View() string {
	if p.styles == nil || p.width == 0 {
		return ""
	}

	innerW := p.panelInnerWidth()

	inputView := p.input.View()
	listView := p.renderList(innerW)
	hint := p.styles.muted.Render("↑↓ navigate · enter run · esc close")

	body := lipgloss.JoinVertical(lipgloss.Left,
		inputView,
		"",
		listView,
		"",
		hint,
	)

	return renderPanel(p.styles, "Command palette", body, p.panelWidth(), true)
}

// panelWidth returns the outer width of the modal palette box. It is sized
// to a fixed comfortable target (52 cols) on wide terminals, and shrinks
// gracefully when the host terminal is narrow.
func (p *paletteScreen) panelWidth() int {
	const target = 52

	if p.width-4 < target {
		return max(p.width-4, 24)
	}

	return target
}

// panelInnerWidth returns the writable width inside the panel borders.
func (p *paletteScreen) panelInnerWidth() int {
	return max(p.panelWidth()-panelContentOffset, 16)
}

func (p *paletteScreen) renderList(rowWidth int) string {
	if len(p.filtered) == 0 {
		return p.renderEmptyState()
	}

	maxRows := p.maxVisibleRows()
	visible := p.filtered

	if len(visible) > maxRows {
		visible = visible[:maxRows]
	}

	// Section headers are only meaningful when the user has not typed a
	// query: a fuzzy match has its own ranking and grouping by Group would
	// only confuse the result list. The empty-query path is also where the
	// rows are pre-sorted by group, so the contiguous-group invariant holds.
	withHeaders := strings.TrimSpace(p.input.Value()) == ""

	rows := make([]string, 0, len(visible)+4)
	prevGroup := ""

	for idx := range visible {
		cmd := &visible[idx]

		if withHeaders && cmd.Group != prevGroup && cmd.Group != "" {
			if prevGroup != "" {
				rows = append(rows, "")
			}

			rows = append(rows, p.styles.paletteGroup.Render(cmd.Group))
			prevGroup = cmd.Group
		}

		rows = append(rows, p.renderRow(cmd, idx == p.cursor, rowWidth))
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (p *paletteScreen) maxVisibleRows() int {
	// Header(2) + blank(1) + input(1) + blank(1) + footer(1) = 6 chrome lines.
	const chrome = 8

	rows := p.height - chrome
	if rows < 3 {
		return 3
	}

	if rows > 12 {
		return 12
	}

	return rows
}

// renderRow formats a single command list entry. Title is left-aligned with
// a cursor prefix; the row is then truncated (never wrapped) to fit
// rowWidth, which the caller derives from the panel inner width. Shortcuts
// are intentionally not rendered here — they live in the help overlay (?)
// and are surfaced via the persistent footer hints, so duplicating them in
// the row would only add noise and risk wrapping.
func (p *paletteScreen) renderRow(cmd *Command, selected bool, rowWidth int) string {
	if rowWidth <= 0 {
		rowWidth = p.panelInnerWidth()
	}

	prefix := "  "
	if selected {
		prefix = "❯ "
	}

	style := p.styles.paletteItem
	if selected {
		style = p.styles.paletteItemSel
	}

	if !cmd.IsEnabled() {
		style = p.styles.paletteDisabled
	}

	row := prefix + cmd.Title

	if width := ansi.StringWidth(row); width < rowWidth {
		row += strings.Repeat(" ", rowWidth-width)
	} else {
		row = ansi.Truncate(row, rowWidth, "…")
	}

	return style.Render(row)
}

func (p *paletteScreen) renderEmptyState() string {
	if p.input.Value() != "" {
		return p.styles.muted.Render("  no commands match \"" + p.input.Value() + "\"")
	}

	lines := []string{
		p.styles.muted.Render("  no commands available"),
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
