// Package pcm16 converts WaxFlow audio buffers to interleaved signed 16-bit PCM.
package pcm16

import (
	"encoding/binary"

	"github.com/colespringer/waxflow/audio"
)

// AppendS16LE appends a buffer's interleaved samples as clipped little-endian int16.
func AppendS16LE(dst []byte, buffer *audio.Buffer) []byte {
	start := len(dst)
	size := buffer.N * buffer.Fmt.Channels * 2
	if cap(dst)-start < size {
		grown := make([]byte, start+size)
		copy(grown, dst)
		dst = grown
	} else {
		dst = dst[:start+size]
	}

	for frame := 0; frame < buffer.N; frame++ {
		for channel := 0; channel < buffer.Fmt.Channels; channel++ {
			sample := buffer.ChanF(channel)[frame]
			if sample > 1 {
				sample = 1
			}
			if sample < -1 {
				sample = -1
			}
			value := int16(sample * 32767)
			offset := start + (frame*buffer.Fmt.Channels+channel)*2
			binary.LittleEndian.PutUint16(dst[offset:], uint16(value))
		}
	}
	return dst
}
