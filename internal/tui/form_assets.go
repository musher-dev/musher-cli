package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/musher-dev/musher-cli/internal/bundledef"
)

// AssetList is a toggleable checkbox list for discovered assets.
type AssetList struct {
	label    string
	items    []assetItem
	cursor   int
	focused  bool
	valState ValState
	touched  bool
	sty      *styles
	keys     *keyMap
}

type assetItem struct {
	asset    bundledef.DiscoveredAsset
	selected bool
}

// NewAssetList creates a new asset list form field.
func NewAssetList(label string, assets []bundledef.DiscoveredAsset, sty *styles, keys *keyMap) *AssetList {
	items := make([]assetItem, len(assets))

	for i, a := range assets {
		items[i] = assetItem{asset: a, selected: true} // all pre-selected
	}

	list := &AssetList{
		label: label,
		items: items,
		sty:   sty,
		keys:  keys,
	}

	list.updateValidation()

	return list
}

// Label implements FormField.
func (list *AssetList) Label() string { return list.label }

// HelpText implements FormField.
func (list *AssetList) HelpText() string {
	return "Skills, agents, and other resources in your bundle"
}

// Focus implements FormField.
func (list *AssetList) Focus() tea.Cmd {
	list.focused = true

	return nil
}

// Blur implements FormField.
func (list *AssetList) Blur() {
	list.focused = false
	list.touched = true
	list.updateValidation()
}

// Focused implements FormField.
func (list *AssetList) Focused() bool { return list.focused }

// Value implements FormField. Returns a summary string.
func (list *AssetList) Value() string {
	count := list.selectedCount()
	if count == 0 {
		return ""
	}

	return fmt.Sprintf("%d selected", count)
}

// SetValue implements FormField (no-op for asset list).
func (list *AssetList) SetValue(_ string) {}

// Validate implements FormField.
func (list *AssetList) Validate() []string {
	list.touched = true
	list.updateValidation()

	if list.selectedCount() == 0 && len(list.items) > 0 {
		return []string{"select at least one asset"}
	}

	return nil
}

// ValidationState implements FormField.
func (list *AssetList) ValidationState() ValState { return list.valState }

// SelectedAssets returns the assets currently selected by the user.
func (list *AssetList) SelectedAssets() []bundledef.Asset {
	var assets []bundledef.Asset

	for _, item := range list.items {
		if item.selected {
			assets = append(assets, item.asset.Asset)
		}
	}

	return assets
}

// SetItems replaces the asset list items (e.g., after async discovery completes).
func (list *AssetList) SetItems(assets []bundledef.DiscoveredAsset) {
	items := make([]assetItem, len(assets))

	for i, a := range assets {
		items[i] = assetItem{asset: a, selected: true}
	}

	list.items = items
	list.cursor = 0
	list.updateValidation()
}

// IsEmpty returns true if there are no items in the list.
func (list *AssetList) IsEmpty() bool { return len(list.items) == 0 }

// View implements FormField.
func (list *AssetList) View(width int) string {
	var view strings.Builder

	count := list.selectedCount()
	icon := statusIcon(list.sty, list.valState)

	if len(list.items) == 0 {
		view.WriteString(list.sty.subtitle.Render(list.label) + " " + icon + "\n")
		view.WriteString("  " + list.sty.placeholder.Render("No assets found in skills/, agents/, prompts/, or tools/."))

		return view.String()
	}

	view.WriteString(list.sty.subtitle.Render(fmt.Sprintf("%s (%d/%d)", list.label, count, len(list.items))) + " " + icon)

	for i, item := range list.items {
		view.WriteString("\n")

		checkbox := "[ ]"
		if item.selected {
			checkbox = "[x]"
		}

		src := item.asset.Src
		kind := item.asset.Kind

		active := list.focused && i == list.cursor

		if active {
			view.WriteString("  " + list.sty.accent.Render(checkbox+" "+src))
		} else {
			var checkStyle string
			if item.selected {
				checkStyle = list.sty.success.Render(checkbox)
			} else {
				checkStyle = list.sty.muted.Render(checkbox)
			}

			view.WriteString("  " + checkStyle + " " + src)
		}

		if kind != "" {
			view.WriteString("  " + list.sty.muted.Render("("+kind+")"))
		}

		if item.asset.IDConflict {
			view.WriteString(" " + list.sty.warning.Render("⚠ ID conflict"))
		}
	}

	return view.String()
}

// Update implements FormField.
func (list *AssetList) Update(msg tea.Msg) (FormField, tea.Cmd) {
	if !list.focused {
		return list, nil
	}

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch {
		case key.Matches(keyMsg, list.keys.Down):
			if list.cursor < len(list.items)-1 {
				list.cursor++
			}
		case key.Matches(keyMsg, list.keys.Up):
			if list.cursor > 0 {
				list.cursor--
			}
		case keyMsg.Code == ' ':
			if list.cursor >= 0 && list.cursor < len(list.items) {
				list.items[list.cursor].selected = !list.items[list.cursor].selected
				list.updateValidation()
			}
		}
	}

	return list, nil
}

func (list *AssetList) selectedCount() int {
	count := 0

	for _, item := range list.items {
		if item.selected {
			count++
		}
	}

	return count
}

func (list *AssetList) updateValidation() {
	switch {
	case len(list.items) == 0:
		list.valState = ValNone
	case list.selectedCount() > 0:
		list.valState = ValValid
	case list.touched:
		list.valState = ValInvalid
	default:
		list.valState = ValNone
	}
}
