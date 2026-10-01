// Package httpstream opens direct HTTP radio streams and removes ICY metadata.
package httpstream

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/media"
	"go.lucor.dev/fynetune/internal/metadata/icy"
	"go.lucor.dev/fynetune/internal/retry"
	"go.lucor.dev/fynetune/internal/version"
)

var (
	ErrHTTPStatus              = errors.New("radio server returned unsuccessful HTTP status")
	ErrUnsupportedStreamFormat = errors.New("unsupported stream format")
	ErrPlaylistUnsupported     = errors.New("radio playlists are not supported")
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
		closeResponseBody(resp.Body)
		statusErr := fmt.Errorf("%w: %d", ErrHTTPStatus, resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooEarly && resp.StatusCode != http.StatusTooManyRequests {
			statusErr = retry.Permanent(statusErr)
		}
		return nil, media.StreamInfo{}, statusErr
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	resolvedURL := url
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	if isPlaylist(contentType, url) || isPlaylist(contentType, resolvedURL) {
		closeResponseBody(resp.Body)
		return nil, media.StreamInfo{}, retry.Permanent(fmt.Errorf("%w: %w (content type %q, URL %q)", ErrUnsupportedStreamFormat, ErrPlaylistUnsupported, contentType, resolvedURL))
	}
	body := bufio.NewReaderSize(resp.Body, 32)
	if resp.ContentLength >= 0 {
		if prefix, _ := body.Peek(16); isPlaylistPrefix(prefix) {
			closeResponseBody(resp.Body)
			return nil, media.StreamInfo{}, retry.Permanent(fmt.Errorf("%w: %w (response body begins with playlist syntax)", ErrUnsupportedStreamFormat, ErrPlaylistUnsupported))
		}
	}
	interval, err := metadataInterval(resp.Header)
	if err != nil {
		closeResponseBody(resp.Body)
		return nil, media.StreamInfo{}, retry.Permanent(err)
	}
	onTitle := func(raw string) {
		if onMetadata != nil {
			onMetadata(raw)
		}
	}
	reader := &streamReader{Reader: icy.NewReader(body, interval, onTitle), closer: resp.Body}
	return reader, media.StreamInfo{URL: resolvedURL, Codec: streamCodec(contentType), MIMEType: contentType}, nil
}

func isPlaylistPrefix(prefix []byte) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf"))
	upper := strings.ToUpper(trimmed)
	return strings.HasPrefix(upper, "#EXTM3U") || strings.HasPrefix(upper, "#EXTINF") ||
		strings.HasPrefix(upper, "[PLAYLIST]") || strings.HasPrefix(upper, "<?XML") || strings.HasPrefix(upper, "<PLAYLIST")
}

func closeResponseBody(body io.Closer) {
	if err := body.Close(); err != nil {
		slog.Warn("could not close rejected radio response", "error", err)
	}
}

func isPlaylist(contentType, rawURL string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		switch strings.ToLower(mediaType) {
		case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl",
			"audio/x-scpls", "application/pls+xml", "application/xspf+xml", "video/x-ms-asf",
			"application/vnd.ms-asf", "application/x-mplayer2", "application/x-ms-wpl":
			return true
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".m3u", ".m3u8", ".pls", ".xspf", ".asx", ".wpl":
		return true
	default:
		return false
	}
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
