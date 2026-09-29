package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/wordmark.svg
var wordmarkSVG []byte

//go:embed assets/icon.svg
var appIconSVG []byte

// The display PNG is rasterized from logo.svg because Fyne's SVG renderer
// silently omits parts of this artwork.
//
//go:embed assets/logo.png
var logoPNG []byte

//go:embed assets/favorite-outline.png
var favoriteOutlineIcon []byte

//go:embed assets/favorite-filled.png
var favoriteFilledIcon []byte

var wordmarkResource = fyne.NewStaticResource("FyneTune-wordmark.svg", wordmarkSVG)
var appIconResource = fyne.NewStaticResource("FyneTune-icon.svg", appIconSVG)
var logoResource = fyne.NewStaticResource("FyneTune-logo.png", logoPNG)
var favoriteOutlineResource = fyne.NewStaticResource("favorite-outline.png", favoriteOutlineIcon)
var favoriteFilledResource = fyne.NewStaticResource("favorite-filled.png", favoriteFilledIcon)
