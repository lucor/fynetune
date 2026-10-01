// Package hls adapts gohlslib's AAC access units to FyneTune's ADTS decoder input.
package hls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/bluenviron/gohlslib/v2"
	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"go.lucor.dev/fynetune/internal/media"
	"go.lucor.dev/fynetune/internal/retry"
)

var (
	ErrNoAACTrack        = errors.New("HLS stream has no AAC audio track")
	ErrUnsupportedAAC    = errors.New("HLS AAC configuration is unsupported")
	ErrInvalidAccessUnit = errors.New("HLS AAC access unit cannot be represented as ADTS")
)

var adtsSampleRates = [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}

type Client struct {
	httpClient *http.Client
}

func New(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) Open(ctx context.Context, uri string) (io.ReadCloser, media.StreamInfo, error) {
	reader, writer := io.Pipe()
	ready := make(chan error, 1)
	var readyOnce sync.Once
	var writeMu sync.Mutex
	var writeErr error

	hlsClient := &gohlslib.Client{}
	*hlsClient = gohlslib.Client{
		URI:        uri,
		HTTPClient: c.httpClient,
		OnTracks: func(tracks []*gohlslib.Track) error {
			var selected *gohlslib.Track
			var audioCodec *codecs.MPEG4Audio
			for _, track := range tracks {
				candidate, ok := track.Codec.(*codecs.MPEG4Audio)
				if !ok {
					continue
				}
				selected = track
				audioCodec = candidate
				if track.IsDefault {
					break
				}
			}
			if selected == nil {
				err := retry.Permanent(ErrNoAACTrack)
				readyOnce.Do(func() { ready <- err })
				return err
			}
			if err := validateAACConfig(audioCodec); err != nil {
				err = retry.Permanent(err)
				readyOnce.Do(func() { ready <- err })
				return err
			}
			hlsClient.OnDataMPEG4Audio(selected, func(_ int64, accessUnits [][]byte) {
				writeMu.Lock()
				defer writeMu.Unlock()
				if writeErr != nil {
					return
				}
				for _, accessUnit := range accessUnits {
					header, err := adtsHeader(audioCodec, len(accessUnit))
					if err != nil {
						err = retry.Permanent(err)
					}
					if err == nil {
						_, err = writer.Write(header[:])
					}
					if err == nil {
						_, err = writer.Write(accessUnit)
					}
					if err != nil {
						writeErr = err
						_ = writer.CloseWithError(err)
						return
					}
				}
			})
			readyOnce.Do(func() { ready <- nil })
			return nil
		},
		OnDecodeError: func(err error) {
			slog.Warn("HLS segment decode warning", "url", uri, "error", err)
		},
	}
	if err := hlsClient.Start(); err != nil {
		_ = reader.Close()
		_ = writer.CloseWithError(err)
		return nil, media.StreamInfo{}, fmt.Errorf("start HLS client: %w", err)
	}
	clientDone := make(chan error, 1)
	go func() { clientDone <- hlsClient.Wait2() }()

	select {
	case err := <-ready:
		if err != nil {
			_ = reader.Close()
			_ = writer.CloseWithError(err)
			hlsClient.Close()
			return nil, media.StreamInfo{}, fmt.Errorf("initialize HLS audio track: %w", err)
		}
	case err := <-clientDone:
		_ = reader.Close()
		_ = writer.CloseWithError(err)
		if ctx.Err() != nil {
			return nil, media.StreamInfo{}, ctx.Err()
		}
		// gohlslib treats every non-fMP4 media segment as MPEG-TS. Some radio
		// CDNs instead publish packed AAC/ADTS segments, which its TS demuxer
		// rejects with EOF before exposing an AAC track. Retry those through the
		// small ADTS-only reader; ordinary HLS stays entirely in gohlslib.
		if errors.Is(err, io.EOF) {
			if stream, info, fallbackErr := c.openPackedAAC(ctx, uri); fallbackErr == nil {
				return stream, info, nil
			} else {
				return nil, media.StreamInfo{}, fmt.Errorf("start HLS playback: %w (packed AAC fallback: %w)", err, fallbackErr)
			}
		}
		return nil, media.StreamInfo{}, fmt.Errorf("start HLS playback: %w", err)
	case <-ctx.Done():
		_ = reader.Close()
		_ = writer.CloseWithError(ctx.Err())
		hlsClient.Close()
		return nil, media.StreamInfo{}, ctx.Err()
	}

	stream := &clientStream{reader: reader, writer: writer, client: hlsClient, closeDone: make(chan struct{})}
	go stream.monitor(ctx, clientDone)
	return stream, media.StreamInfo{URL: uri, Codec: "AAC", MIMEType: "application/vnd.apple.mpegurl"}, nil
}

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

func sampleRateIndex(rate int) int {
	for i, supported := range adtsSampleRates {
		if rate == supported {
			return i
		}
	}
	return -1
}

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

type clientStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	client *gohlslib.Client

	closeOnce sync.Once
	closeDone chan struct{}
}

func (s *clientStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

func (s *clientStream) Close() error {
	s.closeOnce.Do(func() {
		_ = s.reader.Close()
		_ = s.writer.CloseWithError(context.Canceled)
		s.client.Close()
		close(s.closeDone)
	})
	<-s.closeDone
	return nil
}

func (s *clientStream) monitor(ctx context.Context, clientDone <-chan error) {
	select {
	case <-ctx.Done():
		_ = s.Close()
	case err := <-clientDone:
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err == nil {
			_ = s.writer.Close()
		} else {
			_ = s.writer.CloseWithError(err)
		}
	}
}
