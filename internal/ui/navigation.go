package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	brandColorName               fyne.ThemeColorName = "fyneTuneNavy"
	brandAccentColorName         fyne.ThemeColorName = "fyneTuneAccent"
	stationMetadataColorName     fyne.ThemeColorName = "stationMetadata"
	stationMetadataSizeName      fyne.ThemeSizeName  = "stationMetadata"
	stationMetadataSizeReduction                     = 1
)

var (
	brandNavy            = color.NRGBA{R: 14, G: 45, B: 91, A: 255}
	brandAccent          = color.NRGBA{R: 11, G: 143, B: 243, A: 255}
	stationMetadataLight = color.NRGBA{R: 64, G: 87, B: 120, A: 255}
	stationMetadataDark  = color.NRGBA{R: 189, G: 204, B: 224, A: 255}
)

type brandTheme struct {
	fyne.Theme
}

type stationRowTextTheme struct {
	fyne.Theme
}

func (t stationRowTextTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding || name == theme.SizeNameLineSpacing {
		return 0
	}
	return t.Theme.Size(name)
}

func (t brandTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == stationMetadataColorName {
		if variant == theme.VariantDark {
			return stationMetadataDark
		}
		return stationMetadataLight
	}
	if name == brandColorName || name == theme.ColorNamePrimary {
		return brandNavy
	}
	if name == brandAccentColorName {
		return brandAccent
	}
	return t.Theme.Color(name, variant)
}

func (t brandTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == stationMetadataSizeName {
		return t.Theme.Size(theme.SizeNameText) - stationMetadataSizeReduction
	}
	return t.Theme.Size(name)
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
