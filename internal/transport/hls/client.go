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
	"strings"
	"time"

	"github.com/bluenviron/gohlslib/v2"
	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/codec/aac"
)

var (
	// ErrNoAACTrack indicates that an HLS playlist has no AAC audio track.
	ErrNoAACTrack = errors.New("HLS stream has no AAC audio track")
	// ErrUnsupportedAAC indicates that an AAC configuration cannot be represented as ADTS.
	ErrUnsupportedAAC = errors.New("HLS AAC configuration is unsupported")
	// ErrInvalidAccessUnit indicates that an AAC access unit cannot fit in an ADTS frame.
	ErrInvalidAccessUnit = errors.New("HLS AAC access unit cannot be represented as ADTS")
)

const hlsStartupTimeout = 10 * time.Second

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
// initialize packed AAC or fragmented MPEG-TS AAC, Open uses the local segment reader.
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
	// Replace gohlslib's default log.Printf callbacks with structured debug logs.
	hlsClient.OnDownloadPrimaryPlaylist = func(rawURL string) {
		slog.Debug("downloading primary HLS playlist", "url", rawURL)
	}
	hlsClient.OnDownloadStreamPlaylist = func(rawURL string) {
		slog.Debug("downloading HLS stream playlist", "url", rawURL)
	}
	hlsClient.OnDownloadSegment = func(rawURL string) {
		slog.Debug("downloading HLS segment", "url", rawURL)
	}
	hlsClient.OnDownloadPart = func(rawURL string) {
		slog.Debug("downloading HLS part", "url", rawURL)
	}
	if err := hlsClient.Start(); err != nil {
		_ = reader.Close()
		_ = writer.CloseWithError(err)
		return nil, codec.StreamInfo{}, fmt.Errorf("start HLS client: %w", err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- hlsClient.Wait2() }()
	startupTimer := time.NewTimer(hlsStartupTimeout)
	defer startupTimer.Stop()

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
		return c.openAfterHLSFailure(ctx, uri, err)
	case <-startupTimer.C:
		_ = reader.Close()
		_ = writer.CloseWithError(context.DeadlineExceeded)
		hlsClient.Close()
		err := fmt.Errorf("HLS stream produced no AAC data within %s", hlsStartupTimeout)
		if ctx.Err() != nil {
			return nil, codec.StreamInfo{}, ctx.Err()
		}
		return c.openAfterHLSFailure(ctx, uri, err)
	case <-ctx.Done():
		_ = reader.Close()
		_ = writer.CloseWithError(ctx.Err())
		hlsClient.Close()
		return nil, codec.StreamInfo{}, ctx.Err()
	}

	go stream.monitor(ctx, clientDone)
	return stream, codec.StreamInfo{URL: uri, Codec: "AAC", MIMEType: "application/vnd.apple.mpegurl"}, nil
}

// openAfterHLSFailure retries startup failures that can be caused by AAC data
// split across PES packets or timestamps that prevent the first AAC unit from
// reaching the caller.
func (c *Client) openAfterHLSFailure(ctx context.Context, uri string, err error) (io.ReadCloser, codec.StreamInfo, error) {
	if !shouldRetryAACSegments(err) {
		return nil, codec.StreamInfo{}, fmt.Errorf("start HLS playback: %w", err)
	}
	stream, info, fallbackErr := c.openAACSegments(ctx, uri)
	if fallbackErr != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("start HLS playback: %w (AAC segment fallback: %w)", err, fallbackErr)
	}
	return stream, info, nil
}

// shouldRetryAACSegments recognizes upstream MPEG-TS startup failures for
// which the local reader can reconstruct AAC frames and bypass HLS timestamps.
func shouldRetryAACSegments(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return errors.Is(err, io.EOF) ||
		(strings.Contains(message, "unable to decode ADTS:") && strings.Contains(message, "invalid frame length")) ||
		strings.Contains(message, "difference between DTS and system time is too big") ||
		strings.Contains(message, "HLS stream produced no AAC data")
}

// openAACSegments reads AAC from packed ADTS or MPEG-TS segments when the
// standard demuxer cannot initialize. AAC bytes remain continuous across PES
// and segment boundaries so the codec framer can reconstruct split frames.
func (c *Client) openAACSegments(ctx context.Context, uri string) (io.ReadCloser, codec.StreamInfo, error) {
	mediaURL, mediaPlaylist, err := c.resolvePackedMediaPlaylist(ctx, uri)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	if mediaPlaylist.Map != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("AAC segment fallback does not support EXT-X-MAP")
	}
	first, firstIndex := newestPackedSegment(mediaPlaylist)
	if first == nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("AAC playlist has no media segments")
	}
	if first.Key != nil && first.Key.Method != playlist.MediaKeyMethodNone {
		return nil, codec.StreamInfo{}, fmt.Errorf("AAC segment fallback does not support encrypted segments")
	}
	segmentURL, err := mediaURL.Parse(first.URI)
	if err != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("resolve AAC segment URL: %w", err)
	}
	firstData, err := c.downloadPackedSegment(ctx, segmentURL.String(), first.ByteRangeStart, first.ByteRangeLength)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	firstData, err = extractSegmentAAC(ctx, firstData)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	// A declared AAC track is not sufficient: reject an initial segment with
	// no complete ADTS frame before returning a live reader to the decoder.
	if _, err := aac.NewFramer(bytes.NewReader(firstData)).ReadFrame(); err != nil {
		return nil, codec.StreamInfo{}, fmt.Errorf("%w: initial AAC data: %w", errNotPackedAAC, err)
	}

	ctx, cancel := context.WithCancel(ctx)
	reader, writer := io.Pipe()
	stream := &packedAACStream{reader: reader, writer: writer, cancel: cancel, done: make(chan struct{})}
	nextSequence := mediaPlaylist.MediaSequence + firstIndex
	go func() {
		defer close(stream.done)
		defer cancel()
		c.runAACSegments(ctx, writer, mediaURL, mediaPlaylist, firstData, nextSequence)
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

// runAACSegments writes the initial segment and follows a live media playlist.
func (c *Client) runAACSegments(
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
				_ = writer.CloseWithError(fmt.Errorf("AAC segment fallback does not support encrypted segments"))
				return
			}
			segmentURL, err := mediaURL.Parse(segment.URI)
			if err != nil {
				_ = writer.CloseWithError(fmt.Errorf("resolve AAC segment URL: %w", err))
				return
			}
			data, err := c.downloadPackedSegment(ctx, segmentURL.String(), segment.ByteRangeStart, segment.ByteRangeLength)
			if err != nil {
				slog.Warn("could not download AAC HLS segment", "url", segmentURL.String(), "error", err)
				continue
			}
			data, err = extractSegmentAAC(ctx, data)
			if err != nil {
				_ = writer.CloseWithError(err)
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
			slog.Warn("could not reload AAC HLS playlist", "url", mediaURL.String(), "error", err)
			continue
		}
		parsed, err := playlist.Unmarshal(body)
		if err != nil {
			slog.Warn("could not parse AAC HLS playlist", "url", finalURL.String(), "error", err)
			continue
		}
		updated, ok := parsed.(*playlist.Media)
		if !ok {
			_ = writer.CloseWithError(fmt.Errorf("AAC media playlist changed type to %T", parsed))
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
