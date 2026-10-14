package hls

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/bluenviron/gohlslib/v2"
	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"go.lucor.dev/fynetune/internal/retry"
)

// clientStream adapts a gohlslib client to an ADTS reader and owns its lifecycle.
type clientStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	client *gohlslib.Client
	url    string
	ready  chan error

	readyOnce sync.Once
	writeMu   sync.Mutex
	writeErr  error

	closeOnce sync.Once
}

// onTracks selects a supported AAC track and registers its data callback.
func (s *clientStream) onTracks(tracks []*gohlslib.Track) error {
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
		err := retry.NoRetry(ErrNoAACTrack)
		s.signalReady(err)
		return err
	}
	if err := validateAACConfig(audioCodec); err != nil {
		err = retry.NoRetry(err)
		s.signalReady(err)
		return err
	}
	s.client.OnDataMPEG4Audio(selected, func(_ int64, accessUnits [][]byte) {
		s.writeAccessUnits(audioCodec, accessUnits)
	})
	return nil
}

// signalReady reports track initialization once to Open.
func (s *clientStream) signalReady(err error) {
	s.readyOnce.Do(func() { s.ready <- err })
}

// writeAccessUnits converts AAC access units to ADTS and writes them to the reader.
func (s *clientStream) writeAccessUnits(audioCodec *codecs.MPEG4Audio, accessUnits [][]byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeErr != nil {
		return
	}
	for _, accessUnit := range accessUnits {
		header, err := adtsHeader(audioCodec, len(accessUnit))
		if err != nil {
			err = retry.NoRetry(err)
		}
		if err == nil {
			// Signal readiness before writing to the pipe: Open can return and
			// the decoder can begin reading without deadlocking this callback.
			s.signalReady(nil)
			_, err = s.writer.Write(header[:])
		}
		if err == nil {
			_, err = s.writer.Write(accessUnit)
		}
		if err != nil {
			s.writeErr = err
			s.signalReady(err)
			_ = s.writer.CloseWithError(err)
			return
		}
	}
}

// onDecodeError logs a segment decoding warning with the stream URL.
func (s *clientStream) onDecodeError(err error) {
	slog.Warn("HLS segment decode warning", "url", s.url, "error", err)
}

// Read reads ADTS audio data from the HLS stream.
func (s *clientStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

// Close stops the HLS client and closes the stream pipes.
func (s *clientStream) Close() error {
	s.closeOnce.Do(func() {
		_ = s.reader.Close()
		_ = s.writer.CloseWithError(context.Canceled)
		s.client.Close()
	})
	return nil
}

// monitor closes the stream when its context is canceled or the HLS client exits.
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
			if shouldRetryAACSegments(err) {
				// A demuxer interruption is a stream failure, even during the
				// decoder's initial probe. Preserve it as a retryable read error.
				err = fmt.Errorf("HLS stream interrupted: %w: %w", io.ErrUnexpectedEOF, err)
			}
			_ = s.writer.CloseWithError(err)
		}
	}
}
