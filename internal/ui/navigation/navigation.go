// Package navigation provides a compact, bottom-oriented navigation widget.
package navigation

import (
	"bytes"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	indicatorWidth  = 28
	indicatorHeight = 2
	// AccentColorName identifies the brand blue used for the selected item.
	AccentColorName fyne.ThemeColorName = "fyneTuneAccent"
)

// Item defines one navigation destination.
type Item struct {
	Icon         fyne.Resource
	SelectedIcon fyne.Resource
	// IconSize overrides the theme's default size when greater than zero.
	IconSize float32
}

// Navigation displays selectable items in an even grid.
type Navigation struct {
	widget.BaseWidget
	Items      []Item
	Selected   int
	OnSelected func(index int)

	buttons []*navButton
}

// New creates a navigation widget from the provided items.
func New(items ...Item) *Navigation {
	n := &Navigation{Items: append([]Item(nil), items...)}
	n.ExtendBaseWidget(n)
	return n
}

// Select changes the active destination and notifies the caller.
func (n *Navigation) Select(index int) {
	if index < 0 || index >= len(n.Items) || index == n.Selected {
		return
	}
	n.Selected = index
	for i, button := range n.buttons {
		button.setSelected(i == index)
	}
	n.Refresh()
	if n.OnSelected != nil {
		n.OnSelected(index)
	}
}

// CreateRenderer builds the grid using standard Fyne containers.
func (n *Navigation) CreateRenderer() fyne.WidgetRenderer {
	n.buttons = make([]*navButton, len(n.Items))
	items := make([]fyne.CanvasObject, len(n.Items))
	for i, item := range n.Items {
		index := i
		button := newNavButton(item, i == n.Selected, func() { n.Select(index) })
		n.buttons[i] = button
		items[i] = button
	}
	columns := len(items)
	if columns == 0 {
		columns = 1
	}
	return widget.NewSimpleRenderer(container.NewGridWithColumns(columns, items...))
}

type navButton struct {
	widget.BaseWidget
	item      Item
	selected  bool
	onTapped  func()
	icon      *widget.Icon
	indicator *canvas.Rectangle
}

// newNavButton builds one icon destination.
func newNavButton(item Item, selected bool, onTapped func()) *navButton {
	button := &navButton{item: item, selected: selected, onTapped: onTapped}
	button.ExtendBaseWidget(button)
	return button
}

// CreateRenderer builds the centered icon and selection indicator.
func (b *navButton) CreateRenderer() fyne.WidgetRenderer {
	b.icon = widget.NewIcon(b.item.Icon)
	b.indicator = canvas.NewRectangle(color.Transparent)
	b.indicator.SetMinSize(fyne.NewSize(indicatorWidth, indicatorHeight))
	icon := fyne.CanvasObject(b.icon)
	if b.item.IconSize > 0 {
		icon = container.NewGridWrap(fyne.NewSquareSize(b.item.IconSize), b.icon)
	}
	content := container.NewVBox(
		container.NewCenter(b.indicator),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(48, 48), container.NewCenter(icon))),
	)
	b.applySelection()
	return widget.NewSimpleRenderer(content)
}

// Tapped selects this navigation destination.
func (b *navButton) Tapped(*fyne.PointEvent) {
	b.onTapped()
}

// setSelected applies the selection state to the item renderer.
func (b *navButton) setSelected(selected bool) {
	if b.selected == selected {
		return
	}
	b.selected = selected
	b.applySelection()
	b.Refresh()
}

// applySelection updates the indicator and optional selected icon.
func (b *navButton) applySelection() {
	if b.icon == nil {
		return
	}
	icon := b.item.Icon
	if b.selected && b.item.SelectedIcon != nil {
		icon = b.item.SelectedIcon
	}
	if isSVG(icon) && (!b.selected || b.item.SelectedIcon == nil) {
		colorName := theme.ColorNameForeground
		if b.selected {
			colorName = AccentColorName
		}
		icon = theme.NewColoredResource(icon, colorName)
	}
	b.icon.SetResource(icon)
	b.indicator.FillColor = color.Transparent
	if b.selected {
		b.indicator.FillColor = theme.Color(AccentColorName)
	}
	b.icon.Refresh()
	b.indicator.Refresh()
}

// isSVG reports whether a resource contains SVG data that Fyne can color by theme.
func isSVG(resource fyne.Resource) bool {
	content := bytes.TrimSpace(resource.Content())
	return bytes.HasPrefix(content, []byte("<svg")) || bytes.HasPrefix(content, []byte("<?xml"))
}
