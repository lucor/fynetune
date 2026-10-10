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
	stationBadgeColorName        fyne.ThemeColorName = "stationBadge"
	stationMetadataSizeName      fyne.ThemeSizeName  = "stationMetadata"
	stationMetadataSizeReduction                     = 1
)

const (
	appearanceSystem = "system"
	appearanceLight  = "light"
	appearanceDark   = "dark"
)

var (
	brandNavy             = color.NRGBA{R: 14, G: 45, B: 91, A: 255}
	brandAccent           = color.NRGBA{R: 11, G: 143, B: 243, A: 255}
	sectionTitleLight     = brandNavy
	sectionTitleDark      = color.NRGBA{R: 238, G: 243, B: 250, A: 255}
	stationMetadataLight  = color.NRGBA{R: 64, G: 87, B: 120, A: 255}
	stationMetadataDark   = color.NRGBA{R: 189, G: 204, B: 224, A: 255}
	stationBadgeLight     = color.NRGBA{R: 232, G: 238, B: 245, A: 255}
	stationBadgeDark      = color.NRGBA{R: 38, G: 60, B: 84, A: 255}
	settingsSurfaceLight  = color.NRGBA{R: 248, G: 250, B: 253, A: 255}
	settingsSurfaceDark   = color.NRGBA{R: 43, G: 55, B: 70, A: 255}
	settingsBorderLight   = color.NRGBA{R: 155, G: 169, B: 188, A: 255}
	settingsBorderDark    = color.NRGBA{R: 105, G: 122, B: 143, A: 255}
	settingsDisabledLight = color.NRGBA{R: 231, G: 237, B: 245, A: 255}
	settingsDisabledDark  = color.NRGBA{R: 55, G: 69, B: 87, A: 255}
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
	if name == stationBadgeColorName {
		if variant == theme.VariantDark {
			return stationBadgeDark
		}
		return stationBadgeLight
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

// compactTextTheme keeps standard labels compact and uses the metadata palette
// for secondary text without changing padding or disabled colors elsewhere.
type compactTextTheme struct {
	fyne.Theme
}

// Color styles low-importance text with the secondary metadata palette.
func (t compactTextTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameDisabled {
		name = stationMetadataColorName
	}
	return t.Theme.Color(name, variant)
}

// Size removes label insets so stacked text and badges share a leading edge.
func (t compactTextTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameInnerPadding {
		return 0
	}
	return t.Theme.Size(name)
}

// settingsCardTheme gives settings cards a distinct surface and clearer controls.
type settingsCardTheme struct {
	fyne.Theme
}

// Color keeps settings cards distinct from the page and improves control contrast.
func (t settingsCardTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	dark := variant == theme.VariantDark
	switch name {
	case theme.ColorNameBackground, theme.ColorNameInputBackground:
		if dark {
			return settingsSurfaceDark
		}
		return settingsSurfaceLight
	case theme.ColorNameInputBorder:
		if dark {
			return settingsBorderDark
		}
		return settingsBorderLight
	case theme.ColorNameDisabled:
		if dark {
			return stationMetadataDark
		}
		return stationMetadataLight
	case theme.ColorNameDisabledButton:
		if dark {
			return settingsDisabledDark
		}
		return settingsDisabledLight
	case theme.ColorNameShadow:
		return color.Transparent
	default:
		return t.Theme.Color(name, variant)
	}
}
