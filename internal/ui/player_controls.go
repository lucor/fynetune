package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

const (
	playerControlSize      = 44
	playerControlIconSize  = 20
	playerTextGap          = 2
	playerVolumeValueWidth = 48
)

type playerControlTheme struct {
	fyne.Theme
}

func (t playerControlTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameButtonRadius:
		return playerControlSize / 2
	case theme.SizeNameInlineIcon:
		return playerControlIconSize
	}
	return t.Theme.Size(name)
}

type playerTrackTheme struct {
	fyne.Theme
}

func (t playerTrackTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.Theme.Color(stationMetadataColorName, variant)
	}
	return t.Theme.Color(name, variant)
}

func (t playerTrackTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameInnerPadding:
		return 0
	case theme.SizeNameText:
		return t.Theme.Size(stationMetadataSizeName)
	}
	return t.Theme.Size(name)
}
