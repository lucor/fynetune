// Package hls adapts HLS audio to the stream format consumed by the player.
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

	"github.com/bluenviron/gohlslib/v2"
	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"go.lucor.dev/fynetune/internal/codec"
	wavcodec "go.lucor.dev/fynetune/internal/codec/aac"
)

var (
	// ErrNoAACTrack indicates that an HLS playlist has no AAC audio track.
	ErrNoAACTrack = errors.New("HLS stream has no AAC audio track")
	// ErrUnsupportedAAC indicates that an AAC configuration cannot be represented as ADTS.
	ErrUnsupportedAAC = errors.New("HLS AAC configuration is unsupported")
	// ErrInvalidAccessUnit indicates that an AAC access unit cannot fit in an ADTS frame.
	ErrInvalidAccessUnit = errors.New("HLS AAC access unit cannot be represented as ADTS")
)

// adtsSampleRates lists the sample rates in the order used by the ADTS index table.
var adtsSampleRates = [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

// Client opens HLS playlists and exposes supported AAC audio as an ADTS stream.
type Client struct {
	httpClient *http.Client
}

// New creates an HLS client that uses the provided HTTP client.
func New(client *http.Client) *Client {
	return &Client{httpClient: client}
}

// Open starts a client for one HLS playlist, selects a supported AAC track,
// adds an ADTS header to each access unit, and returns the result as an
// io.ReadCloser. Closing the reader stops the HLS session. If gohlslib cannot
// read a packed AAC playlist, Open retries it through the local packed-AAC reader.
func (c *Client) Open(ctx context.Context, uri string) (io.ReadCloser, codec.StreamInfo, error) {
	reader, writer := io.Pipe()
	hlsClient := &gohlslib.Client{URI: uri, HTTPClient: c.httpClient}
	stream := &clientStream{
		reader: reader,
		writer: writer,
		client: hlsClient,
		url:    uri,
		ready:  make(chan error, 1),
	}
	hlsClient.OnTracks = stream.onTracks
	hlsClient.OnDecodeError = stream.onDecodeError
	if err := hlsClient.Start(); err != nil {
		_ = reader.Close()
		_ = writer.CloseWithError(err)
		return nil, codec.StreamInfo{}, fmt.Errorf("start HLS client: %w", err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- hlsClient.Wait2() }()

	select {
	case err := <-stream.ready:
		if err != nil {
			_ = reader.Close()
			_ = writer.CloseWithError(err)
			hlsClient.Close()
			return nil, codec.StreamInfo{}, fmt.Errorf("initialize HLS audio track: %w", err)
		}
	case err := <-clientDone:
		_ = reader.Close()
		_ = writer.CloseWithError(err)
		if ctx.Err() != nil {
			return nil, codec.StreamInfo{}, ctx.Err()
		}
		// gohlslib treats every non-fMP4 media segment as MPEG-TS. Some radio
		// CDNs instead publish packed AAC/ADTS segments, which its TS demuxer
		// rejects with EOF before exposing an AAC track. Retry those through the
		// small ADTS-only reader; ordinary HLS stays entirely in gohlslib.
		if errors.Is(err, io.EOF) {
			if stream, info, fallbackErr := c.openPackedAAC(ctx, uri); fallbackErr == nil {
				return stream, info, nil
			} else {
				return nil, codec.StreamInfo{}, fmt.Errorf("start HLS playback: %w (packed AAC fallback: %w)", err, fallbackErr)
			}
		}
		return nil, codec.StreamInfo{}, fmt.Errorf("start HLS playback: %w", err)
	case <-ctx.Done():
		_ = reader.Close()
		_ = writer.CloseWithError(ctx.Err())
		hlsClient.Close()
		return nil, codec.StreamInfo{}, ctx.Err()
	}

	go stream.monitor(ctx, clientDone)
	return stream, codec.StreamInfo{URL: uri, Codec: "AAC", MIMEType: "application/vnd.apple.mpegurl"}, nil
}

// openPackedAAC reads packed AAC segments when gohlslib cannot demux them.
func (c *Client) openPackedAAC(ctx context.Context, uri string) (io.ReadCloser, codec.StreamInfo, error) {
	mediaURL, mediaPlaylist, err := c.resolvePackedMediaPlaylist(ctx, uri)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	if mediaPlaylist.Map != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("packed AAC fallback does not support EXT-X-MAP")
	}
	first, firstIndex := newestPackedSegment(mediaPlaylist)
	if first == nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("packed AAC playlist has no media segments")
	}
	if first.Key != nil && first.Key.Method != playlist.MediaKeyMethodNone {
		return nil, codec.StreamInfo{}, fmt.Errorf("packed AAC fallback does not support encrypted segments")
	}
	segmentURL, err := mediaURL.Parse(first.URI)
	if err != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("resolve packed AAC segment URL: %w", err)
	}
	firstData, err := c.downloadPackedSegment(ctx, segmentURL.String(), first.ByteRangeStart, first.ByteRangeLength)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	if _, err := wavcodec.NewFramer(bytes.NewReader(firstData)).ReadFrame(); err != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("%w: %v", errNotPackedAAC, err)
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
	return stream, codec.StreamInfo{URL: uri, Codec: "AAC", MIMEType: "application/vnd.apple.mpegurl"}, nil
}

// resolvePackedMediaPlaylist follows a master playlist to an AAC media playlist.
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

// downloadPackedPlaylist fetches a bounded playlist and returns its final URL.
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

// downloadPackedSegment fetches one bounded AAC segment, honoring byte ranges.
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

// runPackedAAC writes the initial segment and follows a live media playlist.
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

// validateAACConfig checks that an AAC configuration can be represented in ADTS.
func validateAACConfig(audioCodec *codecs.MPEG4Audio) error {
	if audioCodec == nil {
		return ErrUnsupportedAAC
	}
	config := &audioCodec.Config
	if config == nil {
		return ErrUnsupportedAAC
	}
	if config.Type != mpeg4audio.ObjectTypeAACLC || config.ChannelConfig < 1 || config.ChannelConfig > 2 || sampleRateIndex(config.SampleRate) < 0 {
		return fmt.Errorf("%w: type=%v sample_rate=%d channel_config=%d", ErrUnsupportedAAC, config.Type, config.SampleRate, config.ChannelConfig)
	}
	return nil
}

// sampleRateIndex returns the ADTS sampling-frequency index, or -1 if unsupported.
func sampleRateIndex(rate int) int {
	for i, supported := range adtsSampleRates {
		if rate == supported {
			return i
		}
	}
	return -1
}

// adtsHeader builds the seven-byte ADTS header for one AAC access unit.
func adtsHeader(audioCodec *codecs.MPEG4Audio, accessUnitSize int) ([7]byte, error) {
	var header [7]byte
	if err := validateAACConfig(audioCodec); err != nil {
		return header, err
	}
	config := &audioCodec.Config
	frameLength := accessUnitSize + len(header)
	if accessUnitSize == 0 || frameLength > 0x1fff {
		return header, fmt.Errorf("%w: access unit size %d", ErrInvalidAccessUnit, accessUnitSize)
	}
	profile := byte(config.Type - 1)
	sampleIndex := byte(sampleRateIndex(config.SampleRate))
	channels := byte(config.ChannelConfig)
	header[0] = 0xff
	header[1] = 0xf1 // MPEG-4, layer 0, no CRC.
	header[2] = profile<<6 | sampleIndex<<2 | channels>>2
	header[3] = channels<<6 | byte(frameLength>>11)&0x03
	header[4] = byte(frameLength >> 3)
	header[5] = byte(frameLength&0x07)<<5 | 0x1f
	header[6] = 0xfc
	return header, nil
}
