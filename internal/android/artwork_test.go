package android

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"

	"go.lucor.dev/fynetune/internal/favicon"
)

func TestAndroidStopDrawableIsWhiteCircleWithTransparentSquare(t *testing.T) {
	data, err := os.ReadFile("res/drawable/ic_stop.xml")
	if err != nil {
		t.Fatal(err)
	}
	var vector struct {
		Width  string `xml:"viewportWidth,attr"`
		Height string `xml:"viewportHeight,attr"`
		Path   struct {
			Fill string `xml:"fillColor,attr"`
			Data string `xml:"pathData,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(data, &vector); err != nil {
		t.Fatal(err)
	}
	if vector.Path.Fill != "#FFFFFFFF" {
		t.Fatalf("Stop fill = %q, want opaque white", vector.Path.Fill)
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s"><path fill="white" d="%s"/></svg>`, vector.Width, vector.Height, vector.Path.Data)
	img, err := renderSVG([]byte(svg))
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []image.Point{{128, 32}, {224, 128}, {128, 224}, {32, 128}} {
		if r, g, b, a := img.At(point.X, point.Y).RGBA(); r != 65535 || g != 65535 || b != 65535 || a != 65535 {
			t.Fatalf("Stop circle is not opaque white at %v", point)
		}
	}
	for _, point := range []image.Point{{128, 128}, {128, 96}, {160, 128}, {128, 160}, {96, 128}} {
		if _, _, _, alpha := img.At(point.X, point.Y).RGBA(); alpha != 0 {
			t.Fatalf("Stop square must be transparent at %v", point)
		}
	}
	if _, _, _, alpha := img.At(10, 10).RGBA(); alpha != 0 {
		t.Fatal("Stop circle must have transparent corners")
	}
}

func TestArtworkPNGPreservesStationArtwork(t *testing.T) {
	solidRed := func(bounds image.Rectangle) image.Image {
		img := image.NewNRGBA(bounds)
		draw.Draw(img, bounds, image.NewUniform(color.NRGBA{R: 255, A: 255}), image.Point{}, draw.Src)
		return img
	}
	for _, tc := range []struct {
		name      string
		icon      *favicon.Icon
		wantSize  image.Point
		wantColor color.NRGBA
	}{
		{"raster is preserved without background or scaling", &favicon.Icon{Raster: solidRed(image.Rect(0, 0, 64, 32))}, image.Pt(64, 32), color.NRGBA{R: 255, A: 255}},
		{"svg is rendered as supplied", &favicon.Icon{SVG: []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><rect width="20" height="10" fill="red"/></svg>`)}, image.Pt(256, 128), color.NRGBA{R: 255, A: 255}},
		{"fallback uses app logo", nil, image.Pt(256, 256), color.NRGBA{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := artworkPNG(tc.icon)
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Size() != tc.wantSize {
				t.Fatalf("artwork size = %v, want %v", img.Bounds().Size(), tc.wantSize)
			}
			if tc.icon != nil {
				pixel := color.NRGBAModel.Convert(img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2)).(color.NRGBA)
				if pixel != tc.wantColor {
					t.Fatalf("artwork color = %v, want %v", pixel, tc.wantColor)
				}
			}
		})
	}
}

func TestArtworkPNGPreservesTransparency(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	source.SetNRGBA(1, 1, color.NRGBA{R: 255, A: 128})
	data, err := artworkPNG(&favicon.Icon{Raster: source})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	pixel := color.NRGBAModel.Convert(img.At(1, 1)).(color.NRGBA)
	if pixel.A != 128 || pixel.R != 255 {
		t.Fatalf("transparent station artwork changed: %v", pixel)
	}
}

func TestArtworkPNGRejectsInvalidImages(t *testing.T) {
	for _, icon := range []favicon.Icon{{}, {SVG: []byte("invalid")}, {SVG: []byte(`<svg viewBox="0 0 0 10"/>`)}} {
		if _, err := artworkPNG(&icon); err == nil {
			t.Fatal("invalid artwork accepted")
		}
	}
}

func TestNotificationDrawableHasVisibleWhiteAlphaMask(t *testing.T) {
	data, err := os.ReadFile("res/drawable-xxxhdpi/ic_notification.png")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Size() != image.Pt(96, 96) {
		t.Fatalf("invalid logo: %v", err)
	}
	visible := 0
	for y := 0; y < 96; y++ {
		for x := 0; x < 96; x++ {
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			if pixel.A != 0 {
				if pixel.R != 255 || pixel.G != 255 || pixel.B != 255 {
					t.Fatalf("notification icon must be white at (%d, %d): %v", x, y, pixel)
				}
				visible++
			}
		}
	}
	if visible < 1000 || visible > 8000 {
		t.Fatalf("notification logo has unsuitable alpha mask: %d pixels", visible)
	}
}
