// Package favicon provides a favicon downloader
package favicon

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/fyne-io/image/ico"

	"go.lucor.dev/fynetune/internal/version"
)

const (
	httpClientTimeout = 5 * time.Second
	sniffDataLen      = 512
)

type Options struct {
	// Client is the HTTP client used to download the favicon. Leave nil to use
	// a client with a five-second timeout.
	Client *http.Client
}

// Download tries to download the favicon with highest resolution for the specified host
// By default it looks into the standard locations
func Download(ctx context.Context, u *url.URL, opts Options) ([]byte, string, error) {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: httpClientTimeout}
	}

	if u.Scheme == "" {
		u.Scheme = "https"
	}

	faviconData, baseURL, err := findFavicon(ctx, client, u)
	if err != nil {
		return nil, "", fmt.Errorf("not found favicon in HTML")
	}
	faviconURL := faviconData.MakeRawURL(baseURL)
	return downloadFavicon(ctx, client, faviconURL)
}

func downloadFavicon(ctx context.Context, client *http.Client, faviconURL string) ([]byte, string, error) {
	res, err := makeRequest(ctx, client, faviconURL)
	if err != nil {
		return nil, "", fmt.Errorf("download: %w", err)
	}
	defer res.Body.Close()

	r := bufio.NewReader(res.Body)

	resContentType := res.Header.Get("Content-Type")

	if resContentType == "image/svg+xml" {
		b, err := io.ReadAll(r)
		return b, "svg", err
	}

	// content-type could be not set or misleading
	// try to sniff the data
	detectedContentType := resContentType
	sniffData, err := r.Peek(sniffDataLen)
	if err == nil {
		detectedContentType = http.DetectContentType(sniffData)
	}

	var img image.Image
	switch detectedContentType {
	case "image/svg+xml":
		b, err := io.ReadAll(r)
		return b, "svg", err
	case "image/x-icon", "image/vnd.microsoft.icon":
		images, err := ico.DecodeAll(r)
		if err == nil {
			img = findBetterSizeFromImages(images)
		}
	case "image/png":
		img, err = png.Decode(r)
	case "image/jpeg":
		// This is a rare case, but some sites provides JPEG
		img, err = jpeg.Decode(r)
	default:
		err = fmt.Errorf("unsupported content type %s detected as %s", res.Header.Get("Content-Type"), detectedContentType)
	}

	if err != nil {
		return nil, "", fmt.Errorf("could not decode favicon: %w", err)
	}

	buf := bytes.Buffer{}
	err = png.Encode(&buf, img)
	return buf.Bytes(), "png", err
}

func makeRequest(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create HTTP request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", version.UserAgent())
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch URL: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid response code: %s", res.Status)
	}
	return res, nil
}

func findFavicon(ctx context.Context, client *http.Client, u *url.URL) (*faviconData, *url.URL, error) {
	res, err := makeRequest(ctx, client, u.String())
	if err != nil {
		return nil, nil, fmt.Errorf("could not fetch URL: %w", err)
	}
	defer res.Body.Close()
	baseURL := res.Request.URL

	favicons := parseHeadSection(res.Body)
	if len(favicons) == 0 {
		fallback := &faviconData{
			href: "/favicon.ico",
			rel:  "icon",
			size: 0,
			mime: "image/x-icon",
		}
		return fallback, baseURL, nil
	}

	return findBetterSize(favicons), baseURL, nil
}

func parseHeadSection(r io.Reader) []*faviconData {
	// Create a new tokenizer from the HTML content
	tokenizer := html.NewTokenizer(r)

	favicons := make([]*faviconData, 0)
	// Iterate through the tokens

	for {
		tokenType := tokenizer.Next()

		switch tokenType {
		case html.ErrorToken:
			return favicons
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data == "link" {
				faviconData := getFaviconData(token)
				if faviconData != nil {
					favicons = append(favicons, faviconData)
				}
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if token.Data == "head" {
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

// getFaviconData checks if the given token is a <link> element with rel
// containg "icon" and returns its href value
func getFaviconData(token html.Token) *faviconData {
	var rel string
	var href string
	var mime string
	var size int

Loop:
	for _, attr := range token.Attr {
		if attr.Key == "rel" {
			switch attr.Val {
			case "icon", "shortcut icon", "apple-touch-icon":
				rel = attr.Val
			default:
				break Loop
			}
		} else if attr.Key == "href" {
			href = attr.Val
		} else if attr.Key == "sizes" {
			v, _, found := strings.Cut(attr.Val, "x")
			if !found {
				continue
			}
			var err error
			size, err = strconv.Atoi(v)
			if err != nil {
				continue
			}
		} else if attr.Key == "type" {
			mime = attr.Val
		}
	}

	if rel == "" {
		return nil
	}

	return &faviconData{
		rel:  rel,
		href: href,
		mime: mime,
		size: size,
	}
}

// findBetterSize finds the closest number to the target in the given slice of integers
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

func findBetterSizeFromImages(images []image.Image) image.Image {
	best := images[0]
	for _, candidate := range images[1:] {
		bestBounds, candidateBounds := best.Bounds(), candidate.Bounds()
		bestArea := int64(bestBounds.Dx()) * int64(bestBounds.Dy())
		candidateArea := int64(candidateBounds.Dx()) * int64(candidateBounds.Dy())
		if candidateArea > bestArea {
			best = candidate
		}
	}
	return best
}
