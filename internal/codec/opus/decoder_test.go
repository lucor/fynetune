package opus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	waxcodec "github.com/colespringer/waxflow/codec"
	waxcontainer "github.com/colespringer/waxflow/container"
	waxogg "github.com/colespringer/waxflow/container/ogg"
)

// TestStreamingPCM checks non-seekable reads, priming, end trim, and chaining.
func TestStreamingPCM(t *testing.T) {
	for _, channels := range []int{1, 2} {
		for _, chains := range []int{1, 2} {
			var stream []byte
			for i := 0; i < chains; i++ {
				stream = append(stream, silenceStream(t, channels, uint32(i+1))...)
			}
			decoder, err := NewDecoder(iotest.OneByteReader(bytes.NewReader(stream)))
			if err != nil {
				t.Fatal(err)
			}
			pcm, err := io.ReadAll(decoder)
			if closeErr := decoder.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
			if err != nil {
				t.Fatal(err)
			}
			want := (4*960 - 120 - 100) * channels * 2 * chains
			if len(pcm) != want || decoder.SampleRate() != 48000 || decoder.Channels() != channels {
				t.Fatalf("channels=%d chains=%d: PCM bytes=%d, want %d; rate=%d channels=%d", channels, chains, len(pcm), want, decoder.SampleRate(), decoder.Channels())
			}
		}
	}
}

func TestTruncatedStream(t *testing.T) {
	stream := silenceStream(t, 2, 1)
	decoder, err := NewDecoder(bytes.NewReader(stream[:len(stream)-1]))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if _, err := io.ReadAll(decoder); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated stream error = %v, want unexpected EOF", err)
	}
}

func TestInvalidHeader(t *testing.T) {
	stream := silenceStream(t, 2, 1)
	stream[30] ^= 1
	if _, err := NewDecoder(bytes.NewReader(stream)); err == nil {
		t.Fatal("corrupt Ogg header must fail checksum validation")
	}
}

// silenceStream writes four 20 ms Opus packets with explicit priming and end trim.
func silenceStream(t *testing.T, channels int, serial uint32) []byte {
	t.Helper()
	head := append([]byte("OpusHead"), 1, byte(channels))
	head = binary.LittleEndian.AppendUint16(head, 120)
	head = binary.LittleEndian.AppendUint32(head, 48000)
	head = binary.LittleEndian.AppendUint16(head, 0)
	head = append(head, 0)
	var out bytes.Buffer
	muxer := waxogg.NewMuxer(&out, &waxogg.MuxerOptions{Serial: serial})
	if err := muxer.Begin([]waxcontainer.Track{{Codec: waxcodec.Opus, CodecConfig: head}}); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := muxer.WritePacket(waxcontainer.Packet{Packet: waxcodec.Packet{Data: []byte{0xf8, 0xff, 0xfe}, Dur: 960, Sync: true}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := muxer.End(waxcodec.Trailer{Samples: 4*960 - 120 - 100, Delay: 120}); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
