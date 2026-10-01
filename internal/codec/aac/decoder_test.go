package aac

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/colespringer/waxflow/audio"
	"github.com/colespringer/waxflow/codec"
	waxaac "github.com/colespringer/waxflow/codec/aac"
	"go.lucor.dev/fynetune/internal/metadata/icy"
	"go.lucor.dev/fynetune/internal/retry"
)

func TestDecoderWaxFlowAACProfiles(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		profile                string
		ps                     bool
		wantRate, wantChannels int
	}{
		{name: "AAC-LC", wantRate: 48000, wantChannels: 2},
		{name: "HE-AAC-v1", profile: "he", wantRate: 48000, wantChannels: 2},
		{name: "HE-AAC-v2", profile: "he", ps: true, wantRate: 48000, wantChannels: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := encodeADTS(t, tc.profile, tc.ps)
			decoder, err := NewDecoder(icy.NewReader(bytes.NewReader(wrapICY(wire, 137)), 137, nil))
			if err != nil {
				t.Fatalf("NewDecoder: %v", err)
			}
			defer decoder.Close()
			if decoder.SampleRate() != tc.wantRate || decoder.Channels() != tc.wantChannels {
				t.Fatalf("output format = %d Hz/%d channels; want %d/%d", decoder.SampleRate(), decoder.Channels(), tc.wantRate, tc.wantChannels)
			}
			pcm, err := io.ReadAll(decoder)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(pcm) == 0 || len(pcm)%(tc.wantChannels*2) != 0 {
				t.Fatalf("PCM bytes = %d", len(pcm))
			}
			nonzero := false
			for _, sample := range pcm {
				nonzero = nonzero || sample != 0
			}
			if !nonzero {
				t.Fatal("decoded PCM is entirely silent")
			}
		})
	}
}

func TestUnsupportedADTSProfileIsPermanent(t *testing.T) {
	first := testADTSFrame([]byte{0x00}, 3, 2)
	first[2] &^= 0xc0 // Main profile instead of the supported AAC-LC core.
	wire := append(first, first...)
	_, err := NewDecoder(bytes.NewReader(wire))
	if err == nil || !retry.IsPermanent(err) {
		t.Fatalf("decoder error = %v, want a permanent unsupported-profile error", err)
	}
}

func wrapICY(audio []byte, interval int) []byte {
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

func encodeADTS(t *testing.T, profile string, ps bool) []byte {
	t.Helper()
	outFormat := audio.Format{Rate: 48000, Channels: 2, Layout: audio.DefaultLayout(2), Type: audio.Float, BitDepth: 32}
	var enc interface {
		Encode(*audio.Buffer, func(codec.Packet) error) error
		Finish(func(codec.Packet) error) (codec.Trailer, error)
	}
	var err error
	if profile == "he" {
		enc, err = waxaac.NewHEEncoder(outFormat, &waxaac.EncoderOptions{Bitrate: 64000, ParametricStereo: ps})
	} else {
		enc, err = waxaac.NewEncoder(outFormat, &waxaac.EncoderOptions{Bitrate: 128000})
	}
	if err != nil {
		t.Fatalf("create fixture encoder: %v", err)
	}
	buf := audio.Get(outFormat, 8192)
	buf.N = 8192
	for c := 0; c < outFormat.Channels; c++ {
		for i := 0; i < buf.N; i++ {
			buf.ChanF(c)[i] = float32(0.4 * math.Sin(2*math.Pi*440*float64(i)/float64(outFormat.Rate)))
		}
	}
	var packets [][]byte
	emit := func(p codec.Packet) error { packets = append(packets, append([]byte(nil), p.Data...)); return nil }
	if err := enc.Encode(buf, emit); err != nil {
		audio.Put(buf)
		t.Fatalf("encode fixture: %v", err)
	}
	audio.Put(buf)
	if _, err := enc.Finish(emit); err != nil {
		t.Fatalf("finish fixture: %v", err)
	}
	coreRate, channels := 48000, 2
	if profile == "he" {
		coreRate = 24000
		if ps {
			channels = 1
		}
	}
	rateIndex := -1
	for i, rate := range adtsSampleRates {
		if rate == coreRate {
			rateIndex = i
			break
		}
	}
	if rateIndex < 0 {
		t.Fatalf("no ADTS index for %d Hz", coreRate)
	}
	var wire []byte
	for _, packet := range packets {
		length := 7 + len(packet)
		header := []byte{0xff, 0xf1, byte(1<<6 | rateIndex<<2 | channels>>2), byte((channels&3)<<6 | length>>11&3), byte(length >> 3), byte(length<<5) | 0x1f, 0xfc}
		wire = append(wire, header...)
		wire = append(wire, packet...)
	}
	if len(packets) < 3 {
		t.Fatalf("fixture encoder emitted %d access units", len(packets))
	}
	return wire
}
