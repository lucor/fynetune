package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	wavcodec "go.lucor.dev/fynetune/internal/codec/aac"
	"go.lucor.dev/fynetune/internal/media"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
)

const (
	maxPackedPlaylistBytes = 512 << 10
	maxPackedSegmentBytes  = 8 << 20
	maxPackedPlaylistDepth = 3
)

var errNotPackedAAC = errors.New("HLS segment is not packed AAC/ADTS")

func (c *Client) openPackedAAC(ctx context.Context, uri string) (io.ReadCloser, media.StreamInfo, error) {
	mediaURL, mediaPlaylist, err := c.resolvePackedMediaPlaylist(ctx, uri)
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	if mediaPlaylist.Map != nil {
		return nil, media.StreamInfo{}, fmt.Errorf("packed AAC fallback does not support EXT-X-MAP")
	}
	first, firstIndex := newestPackedSegment(mediaPlaylist)
	if first == nil {
		return nil, media.StreamInfo{}, fmt.Errorf("packed AAC playlist has no media segments")
	}
	if first.Key != nil && first.Key.Method != playlist.MediaKeyMethodNone {
		return nil, media.StreamInfo{}, fmt.Errorf("packed AAC fallback does not support encrypted segments")
	}
	segmentURL, err := mediaURL.Parse(first.URI)
	if err != nil {
		return nil, media.StreamInfo{}, fmt.Errorf("resolve packed AAC segment URL: %w", err)
	}
	firstData, err := c.downloadPackedSegment(ctx, segmentURL.String(), first.ByteRangeStart, first.ByteRangeLength)
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	if _, err := wavcodec.NewFramer(bytes.NewReader(firstData)).ReadFrame(); err != nil {
		return nil, media.StreamInfo{}, fmt.Errorf("%w: %v", errNotPackedAAC, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	reader, writer := io.Pipe()
	stream := &packedAACStream{reader: reader, writer: writer, cancel: cancel, done: make(chan struct{})}
	nextSequence := mediaPlaylist.MediaSequence + firstIndex
	go func() {
		defer close(stream.done)
		defer cancel()
		c.runPackedAAC(ctx, writer, mediaURL, mediaPlaylist, firstData, nextSequence)
	}()
	return stream, media.StreamInfo{URL: uri, Codec: "AAC", MIMEType: "application/vnd.apple.mpegurl"}, nil
}

func (c *Client) resolvePackedMediaPlaylist(ctx context.Context, rawURL string) (*url.URL, *playlist.Media, error) {
	current := rawURL
	for depth := 0; depth <= maxPackedPlaylistDepth; depth++ {
		data, finalURL, err := c.downloadPackedPlaylist(ctx, current)
		if err != nil {
			return nil, nil, err
		}
		parsed, err := playlist.Unmarshal(data)
		if err != nil {
			return nil, nil, fmt.Errorf("parse HLS playlist %q: %w", current, err)
		}
		switch pl := parsed.(type) {
		case *playlist.Media:
			return finalURL, pl, nil
		case *playlist.Multivariant:
			if depth == maxPackedPlaylistDepth {
				return nil, nil, fmt.Errorf("packed AAC playlist nesting exceeds %d levels", maxPackedPlaylistDepth)
			}
			variant := selectAACVariant(pl.Variants)
			if variant == nil {
				return nil, nil, fmt.Errorf("HLS master playlist has no AAC variant")
			}
			next, err := finalURL.Parse(variant.URI)
			if err != nil {
				return nil, nil, fmt.Errorf("resolve HLS variant URL: %w", err)
			}
			current = next.String()
		default:
			return nil, nil, fmt.Errorf("unsupported HLS playlist type %T", parsed)
		}
	}
	return nil, nil, fmt.Errorf("packed AAC playlist nesting limit exceeded")
}

func selectAACVariant(variants []*playlist.MultivariantVariant) *playlist.MultivariantVariant {
	var audio []*playlist.MultivariantVariant
	for _, variant := range variants {
		for _, codec := range variant.Codecs {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(codec)), "mp4a.") {
				audio = append(audio, variant)
				break
			}
		}
	}
	if len(audio) == 0 {
		audio = variants
	}
	sort.SliceStable(audio, func(i, j int) bool { return audio[i].Bandwidth > audio[j].Bandwidth })
	if len(audio) == 0 {
		return nil
	}
	return audio[0]
}

func newestPackedSegment(pl *playlist.Media) (*playlist.MediaSegment, int) {
	if len(pl.Segments) == 0 {
		return nil, 0
	}
	// Start close to the live edge, while retaining enough AAC frames for the
	// decoder's SBR/PS probe and avoiding a large startup backlog.
	index := max(0, len(pl.Segments)-3)
	return pl.Segments[index], index
}

func (c *Client) downloadPackedPlaylist(ctx context.Context, rawURL string) ([]byte, *url.URL, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("create HLS playlist request: %w", err)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("request HLS playlist %q: %w", rawURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("request HLS playlist %q: HTTP status %d", rawURL, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPackedPlaylistBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read HLS playlist %q: %w", rawURL, err)
	}
	if len(body) > maxPackedPlaylistBytes {
		return nil, nil, fmt.Errorf("HLS playlist exceeds %d bytes", maxPackedPlaylistBytes)
	}
	var finalURL *url.URL
	if response.Request != nil {
		finalURL = response.Request.URL
	}
	if finalURL == nil {
		finalURL, err = url.Parse(rawURL)
		if err != nil {
			return nil, nil, fmt.Errorf("parse HLS playlist URL: %w", err)
		}
	}
	return body, finalURL, nil
}

func (c *Client) downloadPackedSegment(ctx context.Context, rawURL string, start, length *uint64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create AAC segment request: %w", err)
	}
	if length != nil {
		if *length == 0 {
			return nil, fmt.Errorf("AAC segment byte range has zero length")
		}
		byteStart := uint64(0)
		if start != nil {
			byteStart = *start
		}
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", byteStart, byteStart+*length-1))
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request AAC segment %q: %w", rawURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("request AAC segment %q: HTTP status %d", rawURL, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPackedSegmentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read AAC segment %q: %w", rawURL, err)
	}
	if len(body) > maxPackedSegmentBytes {
		return nil, fmt.Errorf("AAC segment exceeds %d bytes", maxPackedSegmentBytes)
	}
	return body, nil
}

func (c *Client) runPackedAAC(
	ctx context.Context,
	writer *io.PipeWriter,
	mediaURL *url.URL,
	current *playlist.Media,
	firstData []byte,
	nextSequence int,
) {
	write := func(data []byte) error {
		_, err := writer.Write(data)
		return err
	}
	if err := write(firstData); err != nil {
		_ = writer.CloseWithError(err)
		return
	}
	nextSequence++

	for {
		if ctx.Err() != nil {
			_ = writer.CloseWithError(ctx.Err())
			return
		}
		for index, segment := range current.Segments {
			sequence := current.MediaSequence + index
			if sequence < nextSequence {
				continue
			}
			if segment.Key != nil && segment.Key.Method != playlist.MediaKeyMethodNone {
				_ = writer.CloseWithError(fmt.Errorf("packed AAC fallback does not support encrypted segments"))
				return
			}
			segmentURL, err := mediaURL.Parse(segment.URI)
			if err != nil {
				_ = writer.CloseWithError(fmt.Errorf("resolve AAC segment URL: %w", err))
				return
			}
			data, err := c.downloadPackedSegment(ctx, segmentURL.String(), segment.ByteRangeStart, segment.ByteRangeLength)
			if err != nil {
				slog.Warn("could not download packed AAC HLS segment", "url", segmentURL.String(), "error", err)
				continue
			}
			if _, err := wavcodec.NewFramer(bytes.NewReader(data)).ReadFrame(); err != nil {
				_ = writer.CloseWithError(fmt.Errorf("%w: %v", errNotPackedAAC, err))
				return
			}
			if err := write(data); err != nil {
				_ = writer.CloseWithError(err)
				return
			}
			nextSequence = sequence + 1
		}

		if current.Endlist {
			_ = writer.Close()
			return
		}
		if err := waitPackedReload(ctx, current.TargetDuration); err != nil {
			_ = writer.CloseWithError(err)
			return
		}
		body, finalURL, err := c.downloadPackedPlaylist(ctx, mediaURL.String())
		if err != nil {
			slog.Warn("could not reload packed AAC HLS playlist", "url", mediaURL.String(), "error", err)
			continue
		}
		parsed, err := playlist.Unmarshal(body)
		if err != nil {
			slog.Warn("could not parse packed AAC HLS playlist", "url", finalURL.String(), "error", err)
			continue
		}
		updated, ok := parsed.(*playlist.Media)
		if !ok {
			_ = writer.CloseWithError(fmt.Errorf("packed AAC media playlist changed type to %T", parsed))
			return
		}
		mediaURL, current = finalURL, updated
		if nextSequence < current.MediaSequence {
			// The live window advanced while a segment was being fetched.
			nextSequence = current.MediaSequence
		}
	}
}

func waitPackedReload(ctx context.Context, targetDuration int) error {
	delay := time.Duration(targetDuration) * time.Second / 2
	if delay < 250*time.Millisecond {
		delay = 250 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type packedAACStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func (s *packedAACStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

func (s *packedAACStream) Close() error {
	s.once.Do(func() {
		s.cancel()
		_ = s.reader.Close()
		_ = s.writer.CloseWithError(context.Canceled)
	})
	<-s.done
	return nil
}
