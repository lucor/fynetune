package favicon

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestDownloadFollowsRedirectAndResolvesIconAgainstFinalURL(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 128, 128))); err != nil {
		t.Fatal(err)
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
			t.Error("discovery must not forward validators from an unrelated resource")
		}
		switch r.URL.Path {
		case "/radio/home.html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="icons/hires.png" type="image/png" sizes="128x128"></head></html>`))
		case "/radio/icons/hires.png":
			http.Redirect(w, r, "/assets/icon.png", http.StatusFound)
		case "/assets/icon.png":
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
	result, err := Download(context.Background(), startURL, Options{Client: redirect.Client(), ETag: `"unrelated"`, LastModified: "unrelated"})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if result.URL != destination.URL+"/assets/icon.png" {
		t.Fatalf("downloaded URL = %q", result.URL)
	}
	icon := result.Icon
	if icon.Raster == nil || len(icon.SVG) != 0 {
		t.Fatalf("downloaded icon = %+v, want raster image", icon)
	}
	if got := icon.Raster.Bounds().Size(); got != image.Pt(128, 128) {
		t.Fatalf("downloaded icon dimensions = %v, want 128x128", got)
	}
}

// TestDownloadURLValidation checks conditional headers, empty 304 bodies, and
// replacement of validators when the server sends a new image.
func TestDownloadURLValidation(t *testing.T) {
	const modified = "Tue, 06 Oct 2026 12:00:00 GMT"
	payload := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	for _, tc := range []struct {
		name         string
		etag         string
		lastModified string
		status       int
		responseETag string
		wantError    bool
	}{
		{name: "etag unchanged", etag: `W/"v1"`, status: http.StatusNotModified},
		{name: "last modified unchanged", lastModified: modified, status: http.StatusNotModified},
		{name: "updated", etag: `"v1"`, lastModified: modified, status: http.StatusOK, responseETag: `"v2"`},
		{name: "unsolicited 304", status: http.StatusNotModified, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("If-None-Match") != tc.etag || r.Header.Get("If-Modified-Since") != tc.lastModified {
					t.Errorf("conditional request = %s %v", r.Method, r.Header)
				}
				if tc.responseETag != "" {
					w.Header().Set("ETag", tc.responseETag)
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write(payload)
				}
			}))
			defer server.Close()
			result, err := DownloadURL(context.Background(), server.URL, Options{
				Client: server.Client(), ETag: tc.etag, LastModified: tc.lastModified,
			})
			if tc.wantError {
				if err == nil {
					t.Fatal("an unsolicited 304 must be rejected")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.URL != server.URL {
				t.Fatalf("source URL = %q, want %q", result.URL, server.URL)
			}
			if tc.status == http.StatusNotModified {
				if !result.NotModified || result.Icon.Raster != nil || len(result.Icon.SVG) != 0 || result.ETag != tc.etag || result.LastModified != tc.lastModified {
					t.Fatalf("304 result = %+v", result)
				}
				return
			}
			if result.NotModified || !bytes.Equal(result.Icon.SVG, payload) || result.ETag != tc.responseETag || result.LastModified != "" {
				t.Fatalf("200 result = %+v", result)
			}
		})
	}
}

func TestDownloadDoesNotMutateURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/icon.png"></head></html>`))
	}))
	defer server.Close()

	u, err := url.Parse(strings.TrimPrefix(server.URL, "http:"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Download(context.Background(), u, Options{Client: server.Client()}); err == nil {
		t.Fatal("Download() error = nil, want HTTPS request failure from the copied URL")
	}
	if u.Scheme != "" {
		t.Fatalf("Download() changed URL scheme to %q", u.Scheme)
	}
}

func TestDecodeReturnsSVGBytes(t *testing.T) {
	data := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`)
	icon, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if icon.Raster != nil || !bytes.Equal(icon.SVG, data) {
		t.Fatalf("Decode() = %+v, want original SVG bytes", icon)
	}
}

func TestDecodeWebP(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
	if err != nil {
		t.Fatal(err)
	}
	icon, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if icon.Raster == nil || icon.Raster.Bounds().Size() != image.Pt(1, 1) {
		t.Fatalf("Decode() raster = %v, want 1x1 WebP", icon.Raster)
	}
}

func TestDecodeRejectsExcessiveDimensions(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, maxIconDimension+1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded.Bytes()); err == nil {
		t.Fatal("Decode() error = nil, want dimension limit error")
	}
}

func TestDownloadRejectsOversizedIcon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxIconBytes+1))
	}))
	defer server.Close()

	if _, err := DownloadURL(context.Background(), server.URL, Options{Client: server.Client()}); err == nil {
		t.Fatal("DownloadURL() error = nil, want size limit error")
	}
}
