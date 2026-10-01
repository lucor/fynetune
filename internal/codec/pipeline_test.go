package codec

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	waxaudio "github.com/colespringer/waxflow/audio"
	waxcodec "github.com/colespringer/waxflow/codec"
	waxaac "github.com/colespringer/waxflow/codec/aac"
	waxmp3 "github.com/colespringer/waxflow/codec/mp3"
	appaudio "go.lucor.dev/fynetune/internal/audio"
	"go.lucor.dev/fynetune/internal/transport/httpstream"
)

func TestHTTPICYDecoderOutputPipeline(t *testing.T) {
	for _, tc := range []struct {
		name, mime string
		fixture    func(*testing.T) []byte
		rate       int
	}{
		{name: "MP3", mime: "audio/mpeg", fixture: mp3RadioFixture, rate: 44100},
		{name: "AAC-LC", mime: "audio/aacp", fixture: aacRadioFixture, rate: 48000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compressed := tc.fixture(t)
			body := interleaveICY(compressed, 257)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Icy-MetaData") != "1" {
					t.Error("request did not negotiate ICY metadata")
				}
				w.Header().Set("Content-Type", tc.mime)
				w.Header().Set("icy-metaint", "257")
				_, _ = w.Write(body)
			}))
			defer server.Close()

			var title string
			stream, info, err := httpstream.New(server.Client()).Open(context.Background(), server.URL, func(value string) { title = value })
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			decoder, err := New().Decode(stream, info)
			if err != nil {
				t.Fatalf("create decoder: %v", err)
			}
			defer decoder.Close()
			output := &captureOutput{}
			playback, err := output.Play(context.Background(), decoder, appaudio.PCMFormat{SampleRate: decoder.SampleRate(), Channels: decoder.Channels(), BitDepth: 16}, 1)
			if err != nil {
				t.Fatalf("PCM output: %v", err)
			}
			defer playback.Close()
			if output.format.SampleRate != tc.rate || output.format.Channels != 2 || output.format.BitDepth != 16 {
				t.Fatalf("PCM format = %+v", output.format)
			}
			nonzero := false
			for _, sample := range output.pcm {
				nonzero = nonzero || sample != 0
			}
			if len(output.pcm) == 0 || !nonzero {
				t.Fatal("output did not receive non-silent PCM")
			}
			if title != "PoC Track" {
				t.Fatalf("ICY title = %q", title)
			}
		})
	}
}

type captureOutput struct {
	format appaudio.PCMFormat
	pcm    []byte
}

func (o *captureOutput) Play(ctx context.Context, source io.Reader, format appaudio.PCMFormat, _ float64) (appaudio.Playback, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o.format = format
	o.pcm = make([]byte, 4096)
	n, err := source.Read(o.pcm)
	o.pcm = o.pcm[:n]
	if err != nil && n == 0 {
		return nil, err
	}
	return &capturePlayback{}, nil
}

type capturePlayback struct{}

func (*capturePlayback) SetVolume(float64) {}
func (*capturePlayback) Err() error        { return nil }
func (*capturePlayback) IsPlaying() bool   { return true }
func (*capturePlayback) Close() error      { return nil }

func mp3RadioFixture(t *testing.T) []byte {
	t.Helper()
	format := waxAudioFormat(44100, 2)
	encoder, err := waxmp3.NewEncoder(format, &waxmp3.EncoderOptions{Bitrate: 128000})
	if err != nil {
		t.Fatal(err)
	}
	input := sineInput(format, 4608)
	var compressed []byte
	emit := func(packet waxcodec.Packet) error { compressed = append(compressed, packet.Data...); return nil }
	if err := encoder.Encode(input, emit); err != nil {
		t.Fatal(err)
	}
	waxaudio.Put(input)
	if _, err := encoder.Finish(emit); err != nil {
		t.Fatal(err)
	}
	return compressed
}

func aacRadioFixture(t *testing.T) []byte {
	t.Helper()
	format := waxAudioFormat(48000, 2)
	encoder, err := waxaac.NewEncoder(format, &waxaac.EncoderOptions{Bitrate: 128000})
	if err != nil {
		t.Fatal(err)
	}
	input := sineInput(format, 8192)
	var accessUnits [][]byte
	emit := func(packet waxcodec.Packet) error {
		accessUnits = append(accessUnits, append([]byte(nil), packet.Data...))
		return nil
	}
	if err := encoder.Encode(input, emit); err != nil {
		t.Fatal(err)
	}
	waxaudio.Put(input)
	if _, err := encoder.Finish(emit); err != nil {
		t.Fatal(err)
	}
	var compressed []byte
	for _, unit := range accessUnits {
		length := 7 + len(unit)
		header := []byte{0xff, 0xf1, 0x4c, byte(0x80 | length>>11&3), byte(length >> 3), byte(length<<5) | 0x1f, 0xfc}
		compressed = append(compressed, header...)
		compressed = append(compressed, unit...)
	}
	return compressed
}

func waxAudioFormat(rate, channels int) waxaudio.Format {
	return waxaudio.Format{Rate: rate, Channels: channels, Layout: waxaudio.DefaultLayout(channels), Type: waxaudio.Float, BitDepth: 32}
}

func sineInput(format waxaudio.Format, frames int) *waxaudio.Buffer {
	buffer := waxaudio.Get(format, frames)
	buffer.N = frames
	for channel := 0; channel < format.Channels; channel++ {
		for i := 0; i < frames; i++ {
			buffer.ChanF(channel)[i] = float32(.35 * math.Sin(2*math.Pi*440*float64(i)/float64(format.Rate)))
		}
	}
	return buffer
}

func interleaveICY(audio []byte, interval int) []byte {
	var body []byte
	for len(audio) > 0 {
		n := min(interval, len(audio))
		body = append(body, audio[:n]...)
		audio = audio[n:]
		if len(audio) > 0 {
			metadata := []byte("StreamTitle='PoC Track';")
			blocks := (len(metadata) + 15) / 16
			body = append(body, byte(blocks))
			for len(metadata) < blocks*16 {
				metadata = append(metadata, 0)
			}
			body = append(body, metadata...)
		}
	}
	return body
}
