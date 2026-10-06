package pcm16

import (
	"bytes"
	"testing"

	"github.com/colespringer/waxflow/audio"
)

// TestAppendS16LEClipsAndInterleavesSamples checks clipping, channel order, and appending.
func TestAppendS16LEClipsAndInterleavesSamples(t *testing.T) {
	format := audio.Format{Rate: 44100, Channels: 2, Layout: audio.DefaultLayout(2), Type: audio.Float, BitDepth: 32}
	buffer := audio.Get(format, 2)
	defer audio.Put(buffer)
	buffer.N = 2
	buffer.ChanF(0)[0], buffer.ChanF(1)[0] = -1, 0.5
	buffer.ChanF(0)[1], buffer.ChanF(1)[1] = -0.5, 2

	got := AppendS16LE([]byte{0xaa}, buffer)
	want := []byte{
		0xaa,
		0x01, 0x80, // -32767
		0xff, 0x3f, // 16383
		0x01, 0xc0, // -16383
		0xff, 0x7f, // clipped to 32767
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("PCM bytes = % x, want % x", got, want)
	}
}
