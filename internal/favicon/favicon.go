// Package favicon discovers and downloads a station's website icon.
package favicon

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	_ "github.com/fyne-io/image/ico"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"

	"go.lucor.dev/fynetune/internal/version"
)

const (
	httpClientTimeout = 5 * time.Second
	maxHTMLBytes      = 1 << 20
	maxIconBytes      = 2 << 20
	maxIconDimension  = 4096
	maxIconPixels     = 16 << 20
)

// Options configures favicon downloads.
type Options struct {
	// Client is the HTTP client used to download the favicon. Leave nil to use
	// a client with a five-second timeout.
	Client *http.Client
	// ETag and LastModified validate the resource passed to DownloadURL.
	// Download discovers a new resource and does not use these validators.
	ETag         string
	LastModified string
}

// DownloadResult describes an image response and its HTTP cache validators.
type DownloadResult struct {
	Icon         Icon
	URL          string
	ETag         string
	LastModified string
	// NotModified indicates a successful validation; Icon is empty in this case.
	NotModified bool
}

// Icon contains either a decoded raster favicon or the original SVG bytes.
// Exactly one field is set.
type Icon struct {
	// Raster contains the decoded raster image, when the icon is not SVG.
	Raster image.Image
	// SVG contains the source SVG bytes, when the icon is vector-based.
	SVG []byte
	// Format is the detected image format, such as png, ico, or svg.
	Format string
	// Size is the source raster's dimensions. It is zero for SVG icons.
	Size image.Point
}

// Download finds and downloads the best icon linked from the specified page.
// If the URL has no scheme, HTTPS is used.
func Download(ctx context.Context, u *url.URL, opts Options) (DownloadResult, error) {
	if u == nil {
		return DownloadResult{}, fmt.Errorf("favicon URL is nil")
	}
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: httpClientTimeout}
	}

	pageURL := *u
	if pageURL.Scheme == "" {
		pageURL.Scheme = "https"
	}
	iconData, baseURL, err := findFavicon(ctx, client, &pageURL)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("find favicon: %w", err)
	}
	return downloadURL(ctx, client, iconData.MakeRawURL(baseURL), Options{})
}

// DownloadURL downloads and decodes an icon from a direct URL.
func DownloadURL(ctx context.Context, rawURL string, opts Options) (DownloadResult, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: httpClientTimeout}
	}
	return downloadURL(ctx, client, rawURL, opts)
}

// downloadURL handles conditional responses before reading and decoding image data.
func downloadURL(ctx context.Context, client *http.Client, rawURL string, opts Options) (DownloadResult, error) {
	res, err := makeRequest(ctx, client, rawURL, opts)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("download favicon: %w", err)
	}
	defer res.Body.Close()
	result := DownloadResult{
		URL:          res.Request.URL.String(),
		ETag:         res.Header.Get("ETag"),
		LastModified: res.Header.Get("Last-Modified"),
	}
	if res.StatusCode == http.StatusNotModified {
		result.NotModified = true
		// A 304 can omit unchanged validators; retain the values sent in the request.
		if result.ETag == "" {
			result.ETag = opts.ETag
		}
		if result.LastModified == "" {
			result.LastModified = opts.LastModified
		}
		return result, nil
	}
	data, err := readLimited(res.Body, maxIconBytes)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("read favicon: %w", err)
	}
	icon, err := decode(data, res.Header.Get("Content-Type"))
	if err != nil {
		contentType := res.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "not provided"
		}
		return DownloadResult{}, fmt.Errorf("decode favicon with Content-Type %q: %w", contentType, err)
	}
	result.Icon = icon
	return result, nil
}

// Decode validates and decodes bounded favicon data for use from the cache.
func Decode(data []byte) (Icon, error) {
	if len(data) > maxIconBytes {
		return Icon{}, fmt.Errorf("favicon exceeds %d-byte limit", maxIconBytes)
	}
	return decode(data, "")
}

func decode(data []byte, contentType string) (Icon, error) {
	if len(data) == 0 {
		return Icon{}, fmt.Errorf("favicon is empty")
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "image/svg+xml") || isSVG(data) {
		return Icon{SVG: bytes.Clone(data), Format: "svg"}, nil
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Icon{}, fmt.Errorf("unsupported or invalid image format: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxIconDimension || config.Height > maxIconDimension || int64(config.Width)*int64(config.Height) > maxIconPixels {
		return Icon{}, fmt.Errorf("%s image dimensions %dx%d are outside the supported limit", format, config.Width, config.Height)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Icon{}, fmt.Errorf("decode %s image: %w", format, err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || bounds.Dx() > maxIconDimension || bounds.Dy() > maxIconDimension || int64(bounds.Dx())*int64(bounds.Dy()) > maxIconPixels {
		return Icon{}, fmt.Errorf("%s image dimensions %dx%d are outside the supported limit", format, bounds.Dx(), bounds.Dy())
	}
	return Icon{Raster: decoded, Format: format, Size: bounds.Size()}, nil
}

func isSVG(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.HasPrefix(trimmed, []byte("<?xml"))
}

// makeRequest sends validators only when supplied for this resource.
func makeRequest(ctx context.Context, client *http.Client, rawURL string, opts Options) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}
	req.Header.Set("Accept", "image/*, text/html;q=0.9, */*;q=0.8")
	req.Header.Set("User-Agent", version.UserAgent())
	if opts.ETag != "" {
		req.Header.Set("If-None-Match", opts.ETag)
	}
	if opts.LastModified != "" {
		req.Header.Set("If-Modified-Since", opts.LastModified)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch URL: %w", err)
	}
	conditional := opts.ETag != "" || opts.LastModified != ""
	if res.StatusCode != http.StatusOK && !(conditional && res.StatusCode == http.StatusNotModified) {
		res.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, res.Status)
	}
	return res, nil
}

func findFavicon(ctx context.Context, client *http.Client, u *url.URL) (*faviconData, *url.URL, error) {
	res, err := makeRequest(ctx, client, u.String(), Options{})
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	page := &io.LimitedReader{R: res.Body, N: maxHTMLBytes + 1}
	baseURL := res.Request.URL
	favicons := parseHeadSection(page)
	if page.N == 0 {
		return nil, nil, fmt.Errorf("homepage HTML exceeds %d-byte limit", maxHTMLBytes)
	}
	if len(favicons) == 0 {
		return &faviconData{href: "/favicon.ico", rel: "icon", mime: "image/x-icon"}, baseURL, nil
	}
	return findBetterSize(favicons), baseURL, nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return data, nil
}

func parseHeadSection(r io.Reader) []*faviconData {
	tokenizer := html.NewTokenizer(r)
	favicons := make([]*faviconData, 0)
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return favicons
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data == "link" {
				if icon := getFaviconData(token); icon != nil {
					favicons = append(favicons, icon)
				}
			}
		case html.EndTagToken:
			if tokenizer.Token().Data == "head" {
				return favicons
			}
		}
	}
}

type faviconData struct {
	href string
	rel  string
	size int
	mime string
}

func (f *faviconData) MakeRawURL(u *url.URL) string {
	reference, err := url.Parse(f.href)
	if err != nil {
		return ""
	}
	return u.ResolveReference(reference).String()
}

func getFaviconData(token html.Token) *faviconData {
	var rel, href, mimeType string
	var size int
	for _, attr := range token.Attr {
		switch attr.Key {
		case "rel":
			for _, relation := range strings.Fields(strings.ToLower(attr.Val)) {
				if relation == "icon" || relation == "apple-touch-icon" {
					rel = attr.Val
					break
				}
			}
		case "href":
			href = strings.TrimSpace(attr.Val)
		case "sizes":
			value, _, found := strings.Cut(strings.ToLower(attr.Val), "x")
			if found {
				size, _ = strconv.Atoi(value)
			}
		case "type":
			mimeType = attr.Val
		}
	}
	if rel == "" || href == "" {
		return nil
	}
	return &faviconData{rel: rel, href: href, mime: mimeType, size: size}
}

// findBetterSize prefers PNG and then the largest declared favicon.
func findBetterSize(favicons []*faviconData) *faviconData {
	best := favicons[0]
	for _, candidate := range favicons[1:] {
		candidatePNG, bestPNG := isPNG(candidate), isPNG(best)
		if candidatePNG && !bestPNG || candidatePNG == bestPNG && candidate.size > best.size {
			best = candidate
		}
	}
	return best
}

func isPNG(favicon *faviconData) bool {
	if strings.EqualFold(favicon.mime, "image/png") {
		return true
	}
	reference, err := url.Parse(favicon.href)
	return err == nil && strings.EqualFold(path.Ext(reference.Path), ".png")
}
