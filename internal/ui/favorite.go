package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log/slog"

	"fyne.io/fyne/v2"
)

const (
	favoriteIconSize  = 20
	favoriteTouchSize = 44
)

var (
	favoriteNeutral    = color.NRGBA{R: 117, G: 133, B: 153, A: 255}
	favoriteRowOutline = tintFavoriteResource(favoriteOutlineResource, favoriteNeutral)
	favoriteRowFilled  = tintFavoriteResource(favoriteFilledResource, brandAccent)
)

// Use the existing PNG's alpha as a mask to preserve its shape and smooth edges.
func tintFavoriteResource(resource fyne.Resource, tint color.NRGBA) fyne.Resource {
	source, err := png.Decode(bytes.NewReader(resource.Content()))
	if err != nil {
		slog.Warn("could not recolor favorite icon", "resource", resource.Name(), "error", err)
		return resource
	}
	result := image.NewNRGBA(source.Bounds())
	for y := result.Bounds().Min.Y; y < result.Bounds().Max.Y; y++ {
		for x := result.Bounds().Min.X; x < result.Bounds().Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			pixel := tint
			pixel.A = uint8(alpha >> 8)
			result.SetNRGBA(x, y, pixel)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, result); err != nil {
		slog.Warn("could not encode favorite icon", "resource", resource.Name(), "error", err)
		return resource
	}
	return fyne.NewStaticResource("row-"+resource.Name(), encoded.Bytes())
}
