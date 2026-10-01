// Package httpstream opens direct HTTP radio streams and removes ICY metadata.
package httpstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/media"
	"go.lucor.dev/fynetune/internal/metadata/icy"
	"go.lucor.dev/fynetune/internal/version"
)

var (
	ErrHTTPStatus              = errors.New("radio server returned unsuccessful HTTP status")
	ErrUnsupportedStreamFormat = errors.New("unsupported radio stream format")
	ErrInvalidICYInterval      = errors.New("invalid ICY metadata interval")
)

type Client struct{ client *http.Client }

func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 15 * time.Second}}
	}
	return &Client{client: client}
}

func (c *Client) Open(ctx context.Context, url string, onMetadata func(string)) (io.ReadCloser, media.StreamInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Icy-MetaData", "1")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, media.StreamInfo{}, fmt.Errorf("%w: %d", ErrHTTPStatus, resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "mpegurl") || strings.Contains(contentType, "application/vnd.apple") {
		_ = resp.Body.Close()
		return nil, media.StreamInfo{}, fmt.Errorf("%w: %s", ErrUnsupportedStreamFormat, contentType)
	}
	interval, err := metadataInterval(resp.Header)
	if err != nil {
		_ = resp.Body.Close()
		return nil, media.StreamInfo{}, err
	}
	onTitle := func(raw string) {
		if onMetadata != nil {
			onMetadata(raw)
		}
	}
	reader := &streamReader{Reader: icy.NewReader(resp.Body, interval, onTitle), closer: resp.Body}
	resolvedURL := url
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	return reader, media.StreamInfo{URL: resolvedURL, Codec: streamCodec(contentType), MIMEType: contentType}, nil
}

func streamCodec(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "MP3"
	}
	switch strings.ToLower(mediaType) {
	case "audio/mpeg", "audio/mp3", "audio/x-mpeg":
		return "MP3"
	case "audio/aac", "audio/aacp", "audio/x-aac":
		return "AAC"
	case "audio/ogg", "application/ogg":
		return "OGG"
	case "audio/opus":
		return "OPUS"
	default:
		if strings.HasPrefix(strings.ToLower(mediaType), "audio/") {
			return strings.ToUpper(strings.TrimPrefix(strings.ToLower(mediaType), "audio/"))
		}
		return "MP3"
	}
}

func (c *Client) CloseIdleConnections() { c.client.CloseIdleConnections() }

func metadataInterval(header http.Header) (int, error) {
	value := header.Get("icy-metaint")
	if value == "" {
		return 0, nil
	}
	interval, err := strconv.Atoi(value)
	if err != nil || interval <= 0 {
		return 0, fmt.Errorf("%w %q", ErrInvalidICYInterval, value)
	}
	return interval, nil
}

type streamReader struct {
	io.Reader
	closer io.Closer
}

func (r *streamReader) Close() error { return r.closer.Close() }
