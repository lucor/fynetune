package ui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"go.lucor.dev/fynetune/internal/ui/navigation"
)

// themeVariantName returns a readable label for Fyne's current theme variant.
func themeVariantName(variant fyne.ThemeVariant) string {
	switch variant {
	case theme.VariantDark:
		return "dark"
	case theme.VariantLight:
		return "light"
	default:
		return fmt.Sprintf("unknown(%d)", variant)
	}
}

const (
	brandColorName               fyne.ThemeColorName = "fyneTuneNavy"
	brandAccentColorName                             = navigation.AccentColorName
	sectionTitleColorName        fyne.ThemeColorName = "sectionTitle"
	stationMetadataColorName     fyne.ThemeColorName = "stationMetadata"
	stationMetadataSizeName      fyne.ThemeSizeName  = "stationMetadata"
	stationMetadataSizeReduction                     = 1
)

const (
	appearanceSystem = "system"
	appearanceLight  = "light"
	appearanceDark   = "dark"
)

var (
	brandNavy            = color.NRGBA{R: 14, G: 45, B: 91, A: 255}
	brandAccent          = color.NRGBA{R: 11, G: 143, B: 243, A: 255}
	sectionTitleLight    = brandNavy
	sectionTitleDark     = color.NRGBA{R: 238, G: 243, B: 250, A: 255}
	stationMetadataLight = color.NRGBA{R: 64, G: 87, B: 120, A: 255}
	stationMetadataDark  = color.NRGBA{R: 189, G: 204, B: 224, A: 255}
)

// brandTheme adds the FyneTune palette and station metadata sizing.
type brandTheme struct {
	fyne.Theme
	appearance string
}

// Color applies the FyneTune palette and station metadata colors.
func (t brandTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch t.appearance {
	case appearanceLight:
		variant = theme.VariantLight
	case appearanceDark:
		variant = theme.VariantDark
	}
	if name == sectionTitleColorName {
		if variant == theme.VariantDark {
			return sectionTitleDark
		}
		return sectionTitleLight
	}
	if name == stationMetadataColorName {
		if variant == theme.VariantDark {
			return stationMetadataDark
		}
		return stationMetadataLight
	}
	if name == brandColorName {
		return brandNavy
	}
	if name == brandAccentColorName || name == theme.ColorNamePrimary {
		return brandAccent
	}
	return t.Theme.Color(name, variant)
}

// Size defines the compact station metadata text size.
func (t brandTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == stationMetadataSizeName {
		return t.Theme.Size(theme.SizeNameText) - stationMetadataSizeReduction
	}
	return t.Theme.Size(name)
}
