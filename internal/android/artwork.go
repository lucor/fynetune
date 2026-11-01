package android

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/ui/assets"
)

// artworkPNG encodes the station artwork as supplied by the player. Android
// Media System UI is responsible for presenting it in the media card.
func artworkPNG(icon *favicon.Icon) ([]byte, error) {
	var source image.Image
	if icon == nil {
		var err error
		source, err = renderSVG(assets.IconSVG)
		if err != nil {
			return nil, err
		}
	} else if len(icon.SVG) != 0 {
		var err error
		source, err = renderSVG(icon.SVG)
		if err != nil {
			return nil, err
		}
	} else {
		source = icon.Raster
	}
	if source == nil || source.Bounds().Empty() {
		return nil, fmt.Errorf("station artwork is empty")
	}
	var data bytes.Buffer
	if err := png.Encode(&data, source); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func renderSVG(data []byte) (result image.Image, err error) {
	// Downloaded SVGs are untrusted; Fyne also guards renderer panics.
	defer func() {
		if recovered := recover(); recovered != nil {
			result, err = nil, fmt.Errorf("could not render station SVG: %v", recovered)
		}
	}()
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := icon.ViewBox.W, icon.ViewBox.H
	if w <= 0 || h <= 0 || math.IsNaN(w) || math.IsNaN(h) || math.IsInf(w, 0) || math.IsInf(h, 0) {
		return nil, fmt.Errorf("station SVG has an invalid view box")
	}
	scale := 256 / math.Max(w, h)
	width, height := max(1, int(w*scale)), max(1, int(h*scale))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	icon.SetTarget(0, 0, float64(width), float64(height))
	icon.Draw(rasterx.NewDasher(width, height, rasterx.NewScannerGV(width, height, img, img.Bounds())), 1)
	return img, nil
}
