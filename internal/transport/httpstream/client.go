// Package httpstream resolves radio playlists, opens HTTP streams, and removes ICY metadata.
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
	"strconv"
	"strings"

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
	ErrPlaylistDepth           = errors.New("radio playlist nesting limit exceeded")
	ErrPlaylistLoop            = errors.New("radio playlist loop detected")
)

const maxPlaylistDepth = 3

type Client struct {
	client *http.Client
	hls    *hlsstream.Client
}

func New(client *http.Client) *Client {
	return &Client{client: client, hls: hlsstream.New(client)}
}

func (c *Client) Open(ctx context.Context, url string, onMetadata func(string)) (io.ReadCloser, media.StreamInfo, error) {
	return c.open(ctx, url, onMetadata, 0, make(map[string]struct{}))
}

func (c *Client) open(ctx context.Context, rawURL string, onMetadata func(string), depth int, seen map[string]struct{}) (io.ReadCloser, media.StreamInfo, error) {
	if depth > maxPlaylistDepth {
		return nil, media.StreamInfo{}, retry.NoRetry(ErrPlaylistDepth)
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
	if err := c.checkStatus(resp); err != nil {
		return nil, media.StreamInfo{}, err
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	resolvedURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	body := bufio.NewReaderSize(resp.Body, 32)
	kind, err := detectPlaylistKind(ctx, body, contentType, resolvedURL)
	if err != nil {
		closeResponseBody(resp.Body)
		return nil, media.StreamInfo{}, err
	}
	if kind != playlist.KindNotPlaylist {
		return c.openPlaylistResponse(ctx, resp.Body, body, kind, contentType, resolvedURL, onMetadata, depth, seen)
	}
	// No playlist was detected, so process the response as a direct stream.
	interval, err := metadataInterval(resp.Header)
	if err != nil {
		closeResponseBody(resp.Body)
		return nil, media.StreamInfo{}, retry.NoRetry(err)
	}
	onTitle := func(raw string) {
		if onMetadata != nil {
			onMetadata(raw)
		}
	}
	reader := &streamReader{Reader: icy.NewReader(body, interval, onTitle), closer: resp.Body}
	return reader, media.StreamInfo{URL: resolvedURL, Codec: streamCodec(contentType), MIMEType: contentType}, nil
}

func (c *Client) openPlaylistResponse(ctx context.Context, responseBody io.ReadCloser, body *bufio.Reader, kind playlist.Kind, contentType, rawURL string, onMetadata func(string), depth int, seen map[string]struct{}) (io.ReadCloser, media.StreamInfo, error) {
	if kind == playlist.KindUnsupported {
		closeResponseBody(responseBody)
		return nil, media.StreamInfo{}, retry.NoRetry(fmt.Errorf("%w: %w (content type %q, URL %q)", ErrUnsupportedStreamFormat, ErrPlaylistUnsupported, contentType, rawURL))
	}
	if depth >= maxPlaylistDepth {
		closeResponseBody(responseBody)
		return nil, media.StreamInfo{}, retry.NoRetry(ErrPlaylistDepth)
	}

	parsed, err := playlist.Parse(body, kind)
	closeResponseBody(responseBody)
	if err != nil {
		if errors.Is(err, playlist.ErrUnsupportedHLS) {
			return c.hls.Open(ctx, rawURL)
		}
		return nil, media.StreamInfo{}, classifyPlaylistError(err)
	}
	return c.resolvePlaylist(ctx, rawURL, parsed.Entries, onMetadata, depth, seen)
}

// resolvePlaylist tries each valid stream URL until one opens or all entries fail.
func (c *Client) resolvePlaylist(ctx context.Context, rawURL string, entries []playlist.Entry, onMetadata func(string), depth int, seen map[string]struct{}) (io.ReadCloser, media.StreamInfo, error) {
	if len(entries) == 0 {
		return nil, media.StreamInfo{}, retry.NoRetry(playlist.ErrNoValidEntries)
	}
	// rawURL comes from a request URL that was already parsed and validated.
	base, _ := url.Parse(rawURL)
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
	resolveErr := fmt.Errorf("resolve playlist %q: %w", rawURL, lastErr)
	if errors.Is(lastErr, playlist.ErrNoValidEntries) || errors.Is(lastErr, ErrPlaylistLoop) || !retry.ShouldRetry(lastErr) {
		return nil, media.StreamInfo{}, retry.NoRetry(resolveErr)
	}
	return nil, media.StreamInfo{}, resolveErr
}

// detectPlaylistKind uses response metadata first, then inspects a short body prefix when needed.
func detectPlaylistKind(ctx context.Context, body *bufio.Reader, contentType, rawURL string) (playlist.Kind, error) {
	kind := playlist.Detect(contentType, rawURL, nil)
	if kind != playlist.KindNotPlaylist || strings.HasPrefix(contentType, "audio/") {
		return kind, nil
	}

	prefix, _ := body.Peek(16)
	if err := ctx.Err(); err != nil {
		return playlist.KindNotPlaylist, err
	}
	return playlist.Detect(contentType, rawURL, prefix), nil
}

// checkStatus closes unsuccessful responses and marks non-retryable statuses.
func (c *Client) checkStatus(resp *http.Response) error {
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	closeResponseBody(resp.Body)
	statusErr := fmt.Errorf("%w: %d", ErrHTTPStatus, resp.StatusCode)
	status := resp.StatusCode
	if status < http.StatusBadRequest || status >= http.StatusInternalServerError {
		return statusErr
	}
	if status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests {
		return statusErr
	}
	return retry.NoRetry(statusErr)
}

func classifyPlaylistError(err error) error {
	if errors.Is(err, playlist.ErrRead) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, playlist.ErrUnsupportedHLS) {
		return retry.NoRetry(fmt.Errorf("%w: %w", ErrUnsupportedStreamFormat, err))
	}
	return retry.NoRetry(err)
}

func closeResponseBody(body io.Closer) {
	if err := body.Close(); err != nil {
		slog.Warn("could not close rejected radio response", "error", err)
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
