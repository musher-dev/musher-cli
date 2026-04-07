package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	configBoolTrue  = "true"
	configBoolFalse = "false"
)

// configInputType classifies config item editing behavior.
type configInputType int

const (
	configInputText     configInputType = iota // Free text.
	configInputBool                            // Toggle true/false.
	configInputDuration                        // Duration string validated on save.
)

// configItem describes a single configuration entry.
type configItem struct {
	key          string
	displayName  string
	description  string
	section      string
	defaultValue string
	value        string
	inputType    configInputType
}

// configScreenState tracks view vs edit mode.
type configScreenState int

const (
	configStateView configScreenState = iota
	configStateEdit
)

// configScreen displays and edits configuration settings.
type configScreen struct {
	config ConfigManager
	apiURL string
	styles *styles
	keys   *keyMap

	width  int
	height int

	state     configScreenState
	focusArea int // 0=list, 1=detail (two-panel only)

	items  []configItem
	cursor int

	// Edit mode.
	editInput textinput.Model
	editErr   string
}

func configItemDefs() []configItem {
	return []configItem{
		{key: "api.url", displayName: "API URL", description: "The base URL for the Musher API.", section: "API", defaultValue: "https://api.musher.dev", inputType: configInputText},
		{key: "network.ca_cert_file", displayName: "CA Certificate", description: "Path to a custom CA certificate bundle.", section: "NETWORK", defaultValue: "", inputType: configInputText},
		{key: "update.auto_apply", displayName: "Auto-apply Updates", description: "Automatically apply CLI updates.", section: "UPDATES", defaultValue: configBoolTrue, inputType: configInputBool},
		{key: "update.check_interval", displayName: "Update Interval", description: "How often to check for updates.", section: "UPDATES", defaultValue: "24h", inputType: configInputDuration},
		{key: "tui.enabled", displayName: "TUI Mode", description: "Enable the interactive TUI on launch.", section: "DISPLAY", defaultValue: configBoolTrue, inputType: configInputBool},
	}
}

func newConfigScreen(config ConfigManager, apiURL string, sty *styles, keys *keyMap) *configScreen {
	items := configItemDefs()

	// Populate current values from config.
	if config != nil {
		for i := range items {
			val := config.GetString(items[i].key)
			if val != "" {
				items[i].value = val
			} else {
				items[i].value = items[i].defaultValue
			}
		}
	} else {
		for i := range items {
			items[i].value = items[i].defaultValue
		}
	}

	editField := textinput.New()
	editField.CharLimit = 256
	sty.applyInputStyles(&editField)

	return &configScreen{
		config:    config,
		apiURL:    apiURL,
		styles:    sty,
		keys:      keys,
		state:     configStateView,
		items:     items,
		editInput: editField,
	}
}

// Init implements Screen.
func (c *configScreen) Init() tea.Cmd {
	return nil
}

// Update implements Screen.
func (c *configScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		c.updateInputWidth()

		return c, nil

	case tea.KeyPressMsg:
		return c.handleKey(msg)
	}

	// Forward to text input when editing.
	if c.state == configStateEdit {
		var cmd tea.Cmd

		c.editInput, cmd = c.editInput.Update(msg)

		return c, cmd
	}

	return c, nil
}

func (c *configScreen) handleKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	if c.state == configStateEdit {
		return c.handleEditKey(msg)
	}

	return c.handleViewKey(msg)
}

func (c *configScreen) handleViewKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, c.keys.Quit):
		return c, tea.Quit

	case key.Matches(msg, c.keys.Back):
		return c, func() tea.Msg { return popScreenMsg{} }

	case key.Matches(msg, c.keys.Up):
		if c.cursor > 0 {
			c.cursor--
		}

		return c, nil

	case key.Matches(msg, c.keys.Down):
		if c.cursor < len(c.items)-1 {
			c.cursor++
		}

		return c, nil

	case key.Matches(msg, c.keys.Enter):
		return c.enterEdit()

	case key.Matches(msg, c.keys.Tab):
		if classifyLayout(c.width) == layoutTwoPanel {
			c.focusArea = (c.focusArea + 1) % 2
		}

		return c, nil

	default:
		// Check for 'r' to reset.
		if msg.Code == 'r' {
			return c.resetCurrent()
		}

		// Check for space to toggle bools.
		if msg.Code == ' ' {
			item := &c.items[c.cursor]
			if item.inputType == configInputBool {
				return c.toggleBool(item)
			}
		}

		return c, nil
	}
}

func (c *configScreen) enterEdit() (Screen, tea.Cmd) {
	item := &c.items[c.cursor]

	if item.inputType == configInputBool {
		return c.toggleBool(item)
	}

	// Text/duration: open text input.
	c.state = configStateEdit
	c.editErr = ""
	c.editInput.SetValue(item.value)
	c.editInput.Placeholder = item.defaultValue

	return c, c.editInput.Focus()
}

func (c *configScreen) toggleBool(item *configItem) (Screen, tea.Cmd) {
	newVal := configBoolTrue
	if item.value == configBoolTrue {
		newVal = configBoolFalse
	}

	item.value = newVal

	if c.config != nil {
		if err := c.config.Set(item.key, newVal); err != nil {
			c.editErr = err.Error()
		}
	}

	return c, nil
}

func (c *configScreen) resetCurrent() (Screen, tea.Cmd) {
	item := &c.items[c.cursor]
	item.value = item.defaultValue

	if c.config != nil {
		if err := c.config.Set(item.key, item.defaultValue); err != nil {
			c.editErr = err.Error()
		}
	}

	return c, nil
}

func (c *configScreen) handleEditKey(msg tea.KeyPressMsg) (Screen, tea.Cmd) {
	switch {
	case key.Matches(msg, c.keys.Back):
		c.state = configStateView
		c.editErr = ""
		c.editInput.Blur()

		return c, nil

	case key.Matches(msg, c.keys.Enter):
		return c.saveEdit()
	}

	// Forward to text input.
	var cmd tea.Cmd

	c.editInput, cmd = c.editInput.Update(msg)

	return c, cmd
}

func (c *configScreen) saveEdit() (Screen, tea.Cmd) {
	item := &c.items[c.cursor]
	newVal := strings.TrimSpace(c.editInput.Value())

	// Validate duration values.
	if item.inputType == configInputDuration && newVal != "" {
		if _, err := time.ParseDuration(newVal); err != nil {
			c.editErr = "Invalid duration: " + newVal

			return c, nil
		}
	}

	item.value = newVal
	c.editErr = ""
	c.state = configStateView
	c.editInput.Blur()

	if c.config != nil {
		if err := c.config.Set(item.key, newVal); err != nil {
			c.editErr = err.Error()
		}
	}

	return c, nil
}

// View implements Screen.
func (c *configScreen) View() string {
	layout := classifyLayout(c.width)

	var content string

	switch layout {
	case layoutTwoPanel:
		content = c.renderTwoPanel()
	case layoutMinimal:
		content = c.renderMinimal()
	default:
		content = c.renderSinglePanel()
	}

	return renderScreenWithHeader(c.width, c.height, c.renderBreadcrumb(), content, c.renderFooter())
}

func (c *configScreen) renderTwoPanel() string {
	panelW := clampMenuWidth(c.width)
	leftContent := c.renderItemList()
	leftPanel := renderPanel(c.styles, "Settings", leftContent, panelW, c.focusArea == 0)

	rightContent := c.renderDetailPane()

	rightW := max(c.width-panelW-panelContentOffset-twoPanelGap, 20)

	rightPanel := renderPanel(c.styles, "Details", rightContent, rightW, c.focusArea == 1)

	gap := strings.Repeat(" ", twoPanelGap)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, gap, rightPanel)
}

func (c *configScreen) renderSinglePanel() string {
	panelW := clampMenuWidth(c.width)
	content := c.renderItemList()

	return renderPanel(c.styles, "Settings", content, panelW, true)
}

func (c *configScreen) renderMinimal() string {
	return c.renderItemList()
}

func (c *configScreen) renderBreadcrumb() string {
	return renderScreenHeader(c.styles, c.width, "", "Configuration")
}

func (c *configScreen) renderItemList() string {
	var buf strings.Builder

	lastSection := ""

	for i, item := range c.items {
		if item.section != lastSection {
			if lastSection != "" {
				buf.WriteString("\n")
			}

			buf.WriteString(c.styles.sectionHeader.Render(item.section))
			buf.WriteString("\n")

			lastSection = item.section
		}

		isActive := i == c.cursor

		if isActive && c.state == configStateEdit {
			c.renderEditItem(&buf, &item)
		} else {
			c.renderViewItem(&buf, &item, isActive)
		}
	}

	return buf.String()
}

// updateInputWidth sizes the edit input to fit the current panel width.
func (c *configScreen) updateInputWidth() {
	pw := clampMenuWidth(c.width)
	inputWidth := pw - panelContentOffset - lipgloss.Width(c.editInput.Prompt)

	if inputWidth > 0 {
		c.editInput.SetWidth(inputWidth)
	}
}

func (c *configScreen) renderEditItem(buf *strings.Builder, item *configItem) {
	buf.WriteString(c.styles.accent.Render("\u276f") + " ")
	buf.WriteString(c.styles.subtitle.Render(item.displayName))
	buf.WriteString("\n")
	buf.WriteString("  " + c.editInput.View())

	if c.editErr != "" {
		buf.WriteString("\n")
		buf.WriteString("  " + c.styles.errStyle.Render(c.editErr))
	}

	if item.defaultValue != "" {
		buf.WriteString("\n")
		buf.WriteString("  " + c.styles.muted.Render("default: "+item.defaultValue))
	}

	buf.WriteString("\n")
}

func (c *configScreen) renderViewItem(buf *strings.Builder, item *configItem, isActive bool) {
	prefix := "  "
	if isActive {
		prefix = c.styles.accent.Render("\u276f") + " "
	}

	displayVal := c.formatItemValue(item)

	buf.WriteString(prefix)
	buf.WriteString(c.styles.subtitle.Render(item.displayName))
	buf.WriteString("\n")
	buf.WriteString("  " + displayVal)
	buf.WriteString("\n")
}

func (c *configScreen) formatItemValue(item *configItem) string {
	if item.value == "" {
		return c.styles.muted.Render("(not set)")
	}

	if item.inputType == configInputBool {
		if item.value == configBoolTrue {
			return c.styles.success.Render(configBoolTrue)
		}

		return c.styles.muted.Render(configBoolFalse)
	}

	return item.value
}

func (c *configScreen) renderDetailPane() string {
	if c.cursor < 0 || c.cursor >= len(c.items) {
		return ""
	}

	item := c.items[c.cursor]

	var buf strings.Builder

	buf.WriteString(c.styles.title.Render(item.displayName))
	buf.WriteString("\n\n")

	if item.description != "" {
		buf.WriteString(item.description)
		buf.WriteString("\n\n")
	}

	buf.WriteString(c.styles.subtitle.Render("Current") + "  " + item.value)
	buf.WriteString("\n")

	if item.defaultValue != "" {
		buf.WriteString(c.styles.subtitle.Render("Default") + "  " + item.defaultValue)
	} else {
		buf.WriteString(c.styles.subtitle.Render("Default") + "  " + c.styles.muted.Render("(empty)"))
	}

	buf.WriteString("\n")

	if item.value != item.defaultValue {
		buf.WriteString("\n")
		buf.WriteString(c.styles.warning.Render("Modified") + " " + c.styles.muted.Render("press r to reset"))
	}

	return buf.String()
}

func (c *configScreen) renderFooter() string {
	var bindings []key.Binding

	if c.state == configStateEdit {
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "discard")),
		}
	} else {
		bindings = []key.Binding{
			key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reset")),
		}

		if classifyLayout(c.width) == layoutTwoPanel {
			bindings = append(bindings, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch")))
		}

		bindings = append(bindings,
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		)
	}

	width := c.width
	if width <= 0 {
		width = 80
	}

	return NewFooter(c.styles, width).Render(FooterContext{
		Bindings:  bindings,
		ShowHints: true,
	})
}
