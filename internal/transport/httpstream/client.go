// Package httpstream resolves radio playlists, opens HTTP streams, and removes ICY metadata.
package httpstream

import (
	"bufio"
	"bytes"
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
	"go.lucor.dev/fynetune/internal/playlist"
	"go.lucor.dev/fynetune/internal/retry"
	hlsstream "go.lucor.dev/fynetune/internal/transport/hls"
	"go.lucor.dev/fynetune/internal/version"
)

var (
	ErrHTTPStatus              = errors.New("radio server returned unsuccessful HTTP status")
	ErrUnsupportedStreamFormat = errors.New("unsupported stream format")
	ErrPlaylistUnsupported     = errors.New("radio playlist format is not supported")
	ErrInvalidICYInterval      = errors.New("invalid ICY metadata interval")
	ErrPlaylistTooLarge        = errors.New("radio playlist exceeds size limit")
	ErrPlaylistDepth           = errors.New("radio playlist nesting limit exceeded")
	ErrPlaylistLoop            = errors.New("radio playlist loop detected")
	ErrPlaylistRead            = errors.New("could not read radio playlist")
)

const (
	maxPlaylistBytes = 512 << 10
	maxPlaylistDepth = 3
)

type Client struct {
	client *http.Client
	hls    *hlsstream.Client
}

func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 15 * time.Second}}
	}
	return &Client{client: client, hls: hlsstream.New(client)}
}

func (c *Client) Open(ctx context.Context, url string, onMetadata func(string)) (io.ReadCloser, media.StreamInfo, error) {
	return c.open(ctx, url, onMetadata, 0, make(map[string]struct{}))
}

func (c *Client) open(ctx context.Context, rawURL string, onMetadata func(string), depth int, seen map[string]struct{}) (io.ReadCloser, media.StreamInfo, error) {
	if depth > maxPlaylistDepth {
		return nil, media.StreamInfo{}, retry.Permanent(ErrPlaylistDepth)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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
	resolvedURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	body := bufio.NewReaderSize(resp.Body, 32)
	kind := playlistKind(contentType, resolvedURL, nil)
	if kind == "" && !strings.HasPrefix(contentType, "audio/") {
		prefix, _ := body.Peek(16)
		if ctx.Err() != nil {
			closeResponseBody(resp.Body)
			return nil, media.StreamInfo{}, ctx.Err()
		}
		kind = playlistKind(contentType, resolvedURL, prefix)
	}
	if kind != "" {
		if kind == "unsupported" {
			closeResponseBody(resp.Body)
			return nil, media.StreamInfo{}, retry.Permanent(fmt.Errorf("%w: %w (content type %q, URL %q)", ErrUnsupportedStreamFormat, ErrPlaylistUnsupported, contentType, resolvedURL))
		}
		if depth >= maxPlaylistDepth {
			closeResponseBody(resp.Body)
			return nil, media.StreamInfo{}, retry.Permanent(ErrPlaylistDepth)
		}
		entries, err := readPlaylist(body, contentType, resolvedURL)
		closeResponseBody(resp.Body)
		if err != nil {
			if errors.Is(err, playlist.ErrUnsupportedHLS) {
				return c.hls.Open(ctx, resolvedURL)
			}
			return nil, media.StreamInfo{}, classifyPlaylistError(err)
		}
		if len(entries) == 0 {
			return nil, media.StreamInfo{}, retry.Permanent(playlist.ErrNoValidEntries)
		}
		base, _ := url.Parse(resolvedURL)
		var lastErr error
		for _, entry := range entries {
			candidate, err := base.Parse(entry.URL)
			if err != nil || candidate.Host == "" || (candidate.Scheme != "http" && candidate.Scheme != "https") {
				continue
			}
			candidate.Fragment = ""
			candidate.RawFragment = ""
			candidateURL := candidate.String()
			if _, exists := seen[candidateURL]; exists {
				lastErr = ErrPlaylistLoop
				continue
			}
			seen[candidateURL] = struct{}{}
			stream, info, err := c.open(ctx, candidateURL, onMetadata, depth+1, seen)
			delete(seen, candidateURL)
			if err == nil {
				return stream, info, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, media.StreamInfo{}, ctx.Err()
			}
		}
		if lastErr == nil {
			lastErr = playlist.ErrNoValidEntries
		}
		resolveErr := fmt.Errorf("resolve playlist %q: %w", resolvedURL, lastErr)
		if errors.Is(lastErr, playlist.ErrNoValidEntries) || errors.Is(lastErr, ErrPlaylistLoop) || retry.IsPermanent(lastErr) {
			return nil, media.StreamInfo{}, retry.Permanent(resolveErr)
		}
		return nil, media.StreamInfo{}, resolveErr
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

func readPlaylist(body io.Reader, contentType, rawURL string) ([]playlist.Entry, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxPlaylistBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPlaylistRead, err)
	}
	if len(data) > maxPlaylistBytes {
		return nil, ErrPlaylistTooLarge
	}
	if playlistFormat(contentType, rawURL, data) == "pls" {
		parsed, err := playlist.ParsePLS(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse PLS playlist: %w", err)
		}
		return parsed.Entries, nil
	}
	parsed, err := playlist.ParseM3U(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse M3U playlist: %w", err)
	}
	return parsed.Entries, nil
}

func classifyPlaylistError(err error) error {
	if errors.Is(err, ErrPlaylistRead) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, playlist.ErrUnsupportedHLS) {
		return retry.Permanent(fmt.Errorf("%w: %w", ErrUnsupportedStreamFormat, err))
	}
	return retry.Permanent(err)
}

func playlistKind(contentType, rawURL string, prefix []byte) string {
	if isPlaylist(contentType, rawURL) {
		return playlistFormat(contentType, rawURL, prefix)
	}
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf"))
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "[PLAYLIST]") {
		return "pls"
	}
	if strings.HasPrefix(upper, "#EXTM3U") || strings.HasPrefix(upper, "#EXTINF") {
		return "m3u"
	}
	return ""
}

func playlistFormat(contentType, rawURL string, prefix []byte) string {
	trimmed := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf")))
	if strings.HasPrefix(trimmed, "[PLAYLIST]") {
		return "pls"
	}
	if strings.HasPrefix(trimmed, "#EXTM3U") || strings.HasPrefix(trimmed, "#EXTINF") {
		return "m3u"
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(mediaType) {
	case "audio/x-scpls":
		return "pls"
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return "m3u"
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		switch strings.ToLower(path.Ext(parsed.Path)) {
		case ".pls":
			return "pls"
		case ".m3u", ".m3u8":
			return "m3u"
		case ".xspf", ".asx", ".wpl":
			return "unsupported"
		}
	}
	if strings.EqualFold(mediaType, "application/pls+xml") || strings.EqualFold(mediaType, "application/xspf+xml") ||
		strings.EqualFold(mediaType, "video/x-ms-asf") || strings.EqualFold(mediaType, "application/vnd.ms-asf") ||
		strings.EqualFold(mediaType, "application/x-mplayer2") || strings.EqualFold(mediaType, "application/x-ms-wpl") {
		return "unsupported"
	}
	return "m3u"
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
