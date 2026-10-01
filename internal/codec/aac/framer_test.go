package aac

import (
	"bytes"
	"io"
	"testing"
)

func testADTSFrame(payload []byte, rateIndex, channels int) []byte {
	length := 7 + len(payload)
	b := []byte{0xff, 0xf1, 0x40 | byte(rateIndex<<2) | byte(channels>>2), byte((channels&3)<<6) | byte(length>>11)&3, byte(length >> 3), byte(length<<5) | 0x1f, 0xfc}
	return append(b, payload...)
}

func TestFramerResyncsPartialReadsAndStripsHeader(t *testing.T) {
	first := testADTSFrame([]byte{1, 2, 3, 4}, 3, 2)
	second := testADTSFrame([]byte{5, 6}, 3, 2)
	wire := append([]byte{0x12, 0xff}, first...)
	wire = append(wire, second...)
	framer := NewFramer(&aacShortReader{reader: bytes.NewReader(wire)})
	for _, want := range [][]byte{{1, 2, 3, 4}, {5, 6}} {
		got, err := framer.ReadFrame()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.AccessUnit, want) || got.SampleRate != 48000 || got.ChannelConfig != 2 || got.HeaderLength != 7 {
			t.Fatalf("frame = %+v, AU %v", got, got.AccessUnit)
		}
	}
	if _, err := framer.ReadFrame(); err != io.EOF {
		t.Fatalf("end error = %v", err)
	}
}

func TestFramerReadsCRCHeader(t *testing.T) {
	frame := testADTSFrame([]byte{0x42, 0x43}, 4, 1)
	frame[1] &^= 1
	length := len(frame) + 2
	frame[3] = frame[3]&0xfc | byte(length>>11)&3
	frame[4] = byte(length >> 3)
	frame[5] = frame[5]&0x1f | byte(length<<5)
	frame = append(frame[:7], append([]byte{0, 0}, frame[7:]...)...)
	got, err := NewFramer(bytes.NewReader(frame)).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if got.HeaderLength != 9 || !bytes.Equal(got.AccessUnit, []byte{0x42, 0x43}) {
		t.Fatalf("frame = %+v, AU %v", got, got.AccessUnit)
	}
}

type aacShortReader struct{ reader io.Reader }

func (r *aacShortReader) Read(p []byte) (int, error) {
	if len(p) > 2 {
		p = p[:2]
	}
	return r.reader.Read(p)
}
