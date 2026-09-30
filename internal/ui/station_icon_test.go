package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"go.lucor.dev/fynetune/internal/radio"
)

func TestDecodeStationIcon(t *testing.T) {
	icon := image.NewRGBA(image.Rect(0, 0, 4, 4))
	icon.Set(1, 1, color.White)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeStationIcon(encoded.Bytes())
	if err != nil || decoded.image == nil || decoded.image.Bounds().Dx() != 4 {
		t.Fatalf("expected decoded 4x4 icon, got %#v (error: %v)", decoded.image, err)
	}
	if _, err := decodeStationIcon([]byte("not an image")); err == nil {
		t.Fatal("malformed image should be rejected with a reason")
	}
	large := image.NewRGBA(image.Rect(0, 0, 1726, 1448))
	encoded.Reset()
	if err := png.Encode(&encoded, large); err != nil {
		t.Fatal(err)
	}
	decoded, err = decodeStationIcon(encoded.Bytes())
	if err != nil || decoded.image.Bounds().Dx() != 1726 || decoded.image.Bounds().Dy() != 1448 {
		t.Fatalf("expected large station image to decode for canvas scaling, got %#v (error: %v)", decoded.image, err)
	}

	tooLarge := image.NewRGBA(image.Rect(0, 0, maxStationIconDimension+1, 1))
	encoded.Reset()
	if err := png.Encode(&encoded, tooLarge); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeStationIcon(encoded.Bytes()); err == nil {
		t.Fatal("unreasonably large image dimensions should be rejected with a reason")
	}
}

func TestDecodeStationIconSVG(t *testing.T) {
	valid := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`)
	icon, err := decodeStationIcon(valid)
	if err != nil || icon.resource == nil {
		t.Fatalf("expected SVG resource, got %#v (error: %v)", icon.resource, err)
	}

	invalid := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><text>Foo & Bar</text></svg>`)
	if _, err := decodeStationIcon(invalid); err == nil || !strings.Contains(err.Error(), "invalid SVG") {
		t.Fatalf("expected malformed SVG error, got %v", err)
	}
}

func TestStationImageKeepsLayoutSize(t *testing.T) {
	test.NewApp()
	w := &Window{faviconMissingLogged: make(map[string]bool)}
	icon := w.stationImage(radio.Station{ID: "station", Name: "Test station"})
	want := fyne.NewSquareSize(stationArtworkSize)
	if got := icon.MinSize(); got != want {
		t.Fatalf("station icon minimum size = %v, want %v", got, want)
	}
}

func TestStationArtworkUsesRoundedSquareFrame(t *testing.T) {
	test.NewApp()
	w := &Window{faviconMissingLogged: make(map[string]bool)}
	icon := w.stationImage(radio.Station{ID: "station", Name: "Test station"})
	frame := stationArtworkFrame(icon)
	if got, want := frame.MinSize(), fyne.NewSquareSize(stationArtworkSize); got != want {
		t.Fatalf("station artwork frame minimum size = %v, want %v", got, want)
	}
	if icon.FillMode != canvas.ImageFillContain {
		t.Fatalf("station artwork fill mode = %v, want contain", icon.FillMode)
	}
	if icon.CornerRadius != stationArtworkCornerRadius {
		t.Fatalf("station artwork corner radius = %v, want %v", icon.CornerRadius, stationArtworkCornerRadius)
	}
}

func TestUpdatePlayingStationIconUpdatesCurrentArtwork(t *testing.T) {
	art := canvas.NewImageFromResource(appIconResource)
	w := &Window{playerArtID: "station", playerArt: art}
	icon := image.NewRGBA(image.Rect(0, 0, 8, 8))

	w.updatePlayingStationIcon("station", stationIcon{image: icon})
	if art.Image != icon {
		t.Fatal("playing station artwork was not updated")
	}

	previous := art.Image
	w.updatePlayingStationIcon("another-station", stationIcon{image: image.NewRGBA(image.Rect(0, 0, 4, 4))})
	if art.Image != previous {
		t.Fatal("artwork for a different station replaced the playing station icon")
	}
}

func TestDownloadStationIconReportsHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadStationIcon(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected HTTP status in error, got %v", err)
	}
}

func TestDownloadStationIconReportsContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	_, err := downloadStationIcon(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), `Content-Type "image/x-icon"`) {
		t.Fatalf("expected response Content-Type in error, got %v", err)
	}
}

func TestDownloadHomepageStationIcon(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 128))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/radio/index.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="icons/favicon.png"></head><body></body></html>`))
		case "/radio/icons/favicon.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(encoded.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	icon, err := downloadHomepageStationIcon(context.Background(), server.Client(), server.URL+"/radio/index.html")
	if err != nil {
		t.Fatalf("download homepage favicon: %v", err)
	}
	if icon.image == nil || icon.image.Bounds().Dx() != 128 || icon.image.Bounds().Dy() != 128 {
		t.Fatalf("expected original 128x128 homepage icon dimensions, got %#v", icon.image)
	}
}

func TestSystemLocaleCountryCode(t *testing.T) {
	t.Setenv("LC_ALL", "it_IT.UTF-8")
	t.Setenv("LC_MESSAGES", "en_GB.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := systemLocaleCountryCode(); got != "IT" {
		t.Fatalf("country code = %q, want IT", got)
	}

	t.Setenv("LC_ALL", "C")
	t.Setenv("LC_MESSAGES", "en_GB.UTF-8")
	if got := systemLocaleCountryCode(); got != "GB" {
		t.Fatalf("fallback country code = %q, want GB", got)
	}

	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "C")
	if got := systemLocaleCountryCode(); got != "" {
		t.Fatalf("country code without locale region = %q, want empty", got)
	}
}
