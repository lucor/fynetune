package mp3

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/colespringer/waxflow/audio"
	"github.com/colespringer/waxflow/codec"
	waxmp3 "github.com/colespringer/waxflow/codec/mp3"
	"go.lucor.dev/fynetune/internal/metadata/icy"
)

func testMP3Frame() []byte {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x64}) // MPEG-1 Layer III, 128 kbit/s, 44.1 kHz.
	return frame
}

func TestFramerResyncsAndReadsShortChunks(t *testing.T) {
	wire := append([]byte{0x00, 0xff, 0x00}, testMP3Frame()...)
	wire = append(wire, testMP3Frame()...)
	framer := NewFramer(&shortReader{reader: bytes.NewReader(wire), max: 1})
	for i := 0; i < 2; i++ {
		frame, header, err := framer.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame(%d): %v", i, err)
		}
		if len(frame) != 417 || header.Rate != 44100 || header.Channels != 2 {
			t.Fatalf("frame/header = %d/%+v", len(frame), header)
		}
	}
	if _, _, err := framer.ReadFrame(); err != io.EOF {
		t.Fatalf("end error = %v, want EOF", err)
	}
}

type shortReader struct {
	reader io.Reader
	max    int
}

func (r *shortReader) Read(p []byte) (int, error) {
	if len(p) > r.max {
		p = p[:r.max]
	}
	return r.reader.Read(p)
}

func TestDecoderProducesPCMThroughWaxFlow(t *testing.T) {
	format := audio.Format{Rate: 44100, Channels: 2, Layout: audio.DefaultLayout(2), Type: audio.Float, BitDepth: 32}
	encoder, err := waxmp3.NewEncoder(format, &waxmp3.EncoderOptions{Bitrate: 128000})
	if err != nil {
		t.Fatal(err)
	}
	input := audio.Get(format, 4608)
	input.N = 4608
	for channel := 0; channel < 2; channel++ {
		for i := 0; i < input.N; i++ {
			input.ChanF(channel)[i] = float32(.3 * math.Sin(2*math.Pi*440*float64(i)/44100))
		}
	}
	var wire []byte
	emit := func(packet codec.Packet) error { wire = append(wire, packet.Data...); return nil }
	if err := encoder.Encode(input, emit); err != nil {
		t.Fatal(err)
	}
	audio.Put(input)
	if _, err := encoder.Finish(emit); err != nil {
		t.Fatal(err)
	}
	if len(wire) == 0 {
		t.Fatal("WaxFlow encoder produced no MP3 frames")
	}
	decoder, err := NewDecoder(icy.NewReader(bytes.NewReader(icyWrap(wire, 128)), 128, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer decoder.Close()
	if decoder.SampleRate() != 44100 || decoder.Channels() != 2 {
		t.Fatalf("format = %d Hz/%d ch", decoder.SampleRate(), decoder.Channels())
	}
	pcm, err := io.ReadAll(decoder)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(pcm) == 0 || len(pcm)%4 != 0 {
		t.Fatalf("PCM bytes = %d", len(pcm))
	}
	nonzero := false
	for _, sample := range pcm {
		nonzero = nonzero || sample != 0
	}
	if !nonzero {
		t.Fatal("decoded PCM is entirely silent")
	}
}

func icyWrap(audio []byte, interval int) []byte {
	var out []byte
	for len(audio) > 0 {
		n := min(interval, len(audio))
		out = append(out, audio[:n]...)
		audio = audio[n:]
		if len(audio) > 0 {
			metadata := []byte("StreamTitle='PoC';")
			blocks := (len(metadata) + 15) / 16
			out = append(out, byte(blocks))
			for len(metadata) < blocks*16 {
				metadata = append(metadata, 0)
			}
			out = append(out, metadata...)
		}
	}
	return out
}
