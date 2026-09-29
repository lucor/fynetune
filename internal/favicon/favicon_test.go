package favicon

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"go.lucor.dev/fynetune/internal/version"
)

func TestFindBetterSizePrefersHighestResolutionPNG(t *testing.T) {
	icons := []*faviconData{
		{href: "/favicon.svg", mime: "image/svg+xml", size: 512},
		{href: "/favicon-32.png", mime: "image/png", size: 32},
		{href: "/favicon-192.png", mime: "image/png", size: 192},
	}
	if got := findBetterSize(icons); got != icons[2] {
		t.Fatalf("selected favicon = %#v, want highest-resolution PNG %#v", got, icons[2])
	}
}

func TestFindBetterSizeUsesLargestNonPNGWhenNoPNGExists(t *testing.T) {
	icons := []*faviconData{
		{href: "/favicon.svg", mime: "image/svg+xml", size: 0},
		{href: "/icon.ico", mime: "image/x-icon", size: 256},
		{href: "/large.svg", mime: "image/svg+xml", size: 512},
	}
	if got := findBetterSize(icons); got != icons[2] {
		t.Fatalf("selected favicon = %#v, want largest icon %#v", got, icons[2])
	}
}

func TestFindBetterSizeFromImagesKeepsHighestResolution(t *testing.T) {
	icons := []image.Image{
		image.NewRGBA(image.Rect(0, 0, 16, 16)),
		image.NewRGBA(image.Rect(0, 0, 128, 128)),
		image.NewRGBA(image.Rect(0, 0, 32, 32)),
	}
	if got := findBetterSizeFromImages(icons); got != icons[1] {
		t.Fatalf("selected image size = %v, want 128x128", got.Bounds().Size())
	}
}

func TestDownloadFollowsRedirectAndResolvesIconAgainstFinalURL(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 128))); err != nil {
		t.Fatal(err)
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		switch r.URL.Path {
		case "/radio/home.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="icons/hires.png" type="image/png" sizes="128x128"></head></html>`))
		case "/radio/icons/hires.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(encoded.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer destination.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		http.Redirect(w, r, destination.URL+"/radio/home.html", http.StatusFound)
	}))
	defer redirect.Close()

	startURL, err := url.Parse(redirect.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	data, format, err := Download(context.Background(), startURL, Options{Client: redirect.Client()})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode downloaded icon config: %v", err)
	}
	if config.Width != 128 || config.Height != 128 {
		t.Fatalf("downloaded icon dimensions = %dx%d, want 128x128", config.Width, config.Height)
	}
}
