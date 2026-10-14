package hls

import (
	"errors"
	"io"
	"testing"

	"github.com/bluenviron/gohlslib/v2/pkg/codecs"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/mpeg4audio"
	"go.lucor.dev/fynetune/internal/retry"
)

// TestADTSHeader checks the ADTS header fields for AAC-LC stereo at 44.1 kHz.
func TestADTSHeader(t *testing.T) {
	codec := &codecs.MPEG4Audio{Config: mpeg4audio.AudioSpecificConfig{
		Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 44100, ChannelConfig: 2,
	}}
	header, err := adtsHeader(codec, 100)
	if err != nil {
		t.Fatal(err)
	}
	if header[0] != 0xff || header[1] != 0xf1 || header[2] != 0x50 || header[3] != 0x80 || header[4] != 0x0d || header[5] != 0x7f || header[6] != 0xfc {
		t.Fatalf("ADTS header = % x", header)
	}
}

// TestFirstInvalidAccessUnitReportsStartupError prevents Open from waiting for
// a timeout when the first access unit cannot be represented as ADTS.
func TestFirstInvalidAccessUnitReportsStartupError(t *testing.T) {
	for _, size := range []int{0, 8192} {
		reader, writer := io.Pipe()
		stream := &clientStream{reader: reader, writer: writer, ready: make(chan error, 1)}
		audioCodec := &codecs.MPEG4Audio{Config: mpeg4audio.AudioSpecificConfig{
			Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 44100, ChannelConfig: 2,
		}}
		stream.writeAccessUnits(audioCodec, [][]byte{make([]byte, size)})
		_ = reader.Close()
		_ = writer.Close()
		select {
		case err := <-stream.ready:
			if !errors.Is(err, ErrInvalidAccessUnit) || retry.ShouldRetry(err) {
				t.Fatalf("size %d: startup error = %v, want non-retryable invalid access unit", size, err)
			}
		default:
			t.Fatalf("size %d: first access unit error did not signal readiness", size)
		}
	}
}

// TestADTSHeaderRejectsUnsupportedConfiguration checks invalid channel layouts are rejected.
func TestADTSHeaderRejectsUnsupportedConfiguration(t *testing.T) {
	codec := &codecs.MPEG4Audio{Config: mpeg4audio.AudioSpecificConfig{
		Type: mpeg4audio.ObjectTypeAACLC, SampleRate: 44100, ChannelConfig: 3,
	}}
	if _, err := adtsHeader(codec, 10); err == nil {
		t.Fatal("expected unsupported channel configuration error")
	}
}
