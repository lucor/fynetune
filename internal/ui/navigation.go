package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const brandColorName fyne.ThemeColorName = "fyneTuneNavy"

var brandNavy = color.NRGBA{R: 97, G: 127, B: 202, A: 255}

type brandTheme struct {
	fyne.Theme
}

func (t brandTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == brandColorName || name == theme.ColorNamePrimary {
		return brandNavy
	}
	return t.Theme.Color(name, variant)
}

type navItem struct {
	widget.BaseWidget
	text             string
	resource         fyne.Resource
	selectedResource fyne.Resource
	selected         bool
	onTapped         func()
	icon             *widget.Icon
	label            *widget.Label
}

func newNavItem(text string, icon, selectedIcon fyne.Resource, onTapped func()) *navItem {
	item := &navItem{text: text, resource: icon, selectedResource: selectedIcon, onTapped: onTapped}
	item.ExtendBaseWidget(item)
	return item
}

func (n *navItem) CreateRenderer() fyne.WidgetRenderer {
	n.icon = widget.NewIcon(n.resource)
	n.label = widget.NewLabel(n.text)
	n.label.Alignment = fyne.TextAlignCenter
	content := container.NewVBox(container.NewCenter(n.icon), container.NewCenter(n.label))
	n.applyStyle()
	return widget.NewSimpleRenderer(content)
}

func (n *navItem) Tapped(*fyne.PointEvent) {
	if n.onTapped != nil {
		n.onTapped()
	}
}

func (n *navItem) SetSelected(selected bool) {
	if n.selected == selected {
		return
	}
	n.selected = selected
	n.applyStyle()
	n.Refresh()
}

func (n *navItem) applyStyle() {
	if n.icon == nil || n.label == nil {
		return
	}
	icon := n.resource
	if n.selected && n.selectedResource != nil {
		icon = n.selectedResource
	}
	if n.selected && isSVG(icon.Content()) {
		icon = theme.NewColoredResource(icon, brandColorName)
	}
	if n.selected {
		n.label.Importance = widget.HighImportance
	} else {
		n.label.Importance = widget.MediumImportance
	}
	n.icon.SetResource(icon)
	n.label.TextStyle.Bold = n.selected
	n.icon.Refresh()
	n.label.Refresh()
}
