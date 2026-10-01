package otooutput

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestResamplerChangesSampleRate(t *testing.T) {
	var source bytes.Buffer
	for _, sample := range []stereoFrame{{left: 0, right: 0}, {left: 1000, right: -1000}} {
		var frame [pcmFrameSize]byte
		binary.LittleEndian.PutUint16(frame[:2], uint16(sample.left))
		binary.LittleEndian.PutUint16(frame[2:], uint16(sample.right))
		source.Write(frame[:])
	}
	resampler := resampler{source: &source, inputRate: 22050, outputRate: 44100}
	out := make([]byte, 12)
	n, err := resampler.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != 8 {
		t.Fatalf("resampled bytes = %d, want 8", n)
	}
	got := []int16{
		int16(binary.LittleEndian.Uint16(out[:2])),
		int16(binary.LittleEndian.Uint16(out[2:4])),
		int16(binary.LittleEndian.Uint16(out[4:6])),
		int16(binary.LittleEndian.Uint16(out[6:8])),
	}
	want := []int16{0, 0, 500, -500}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample %d = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestMonoToStereoDuplicatesSamples(t *testing.T) {
	var source bytes.Buffer
	for _, value := range []int16{-1200, 3400} {
		var sample [2]byte
		binary.LittleEndian.PutUint16(sample[:], uint16(value))
		source.Write(sample[:])
	}
	out := make([]byte, 8)
	reader := &monoToStereo{source: &source}
	n, err := reader.Read(out)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(out) {
		t.Fatalf("read %d bytes, want %d", n, len(out))
	}
	want := []int16{-1200, -1200, 3400, 3400}
	for i, value := range want {
		if got := int16(binary.LittleEndian.Uint16(out[i*2:])); got != value {
			t.Errorf("sample %d = %d, want %d", i, got, value)
		}
	}
}
