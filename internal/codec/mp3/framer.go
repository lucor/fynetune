// Package mp3 frames and decodes MPEG Layer III audio streams.
package mp3

import (
	"bufio"
	"io"

	waxmp3 "github.com/colespringer/waxflow/codec/mp3"
)

// maxFrameSize bounds accepted MP3 frame lengths while searching for sync.
const maxFrameSize = 8 << 10

// Framer finds complete Layer III frames in a possibly misaligned or chunked reader.
type Framer struct{ r *bufio.Reader }

// NewFramer creates a framer with buffering for one maximum-size frame and lookahead.
func NewFramer(r io.Reader) *Framer {
	return &Framer{r: bufio.NewReaderSize(r, maxFrameSize+waxmp3.HeaderLen)}
}

// ReadFrame finds and returns the next complete Layer III frame and its header.
func (f *Framer) ReadFrame() ([]byte, waxmp3.Header, error) {
	for {
		candidate, err := f.r.Peek(waxmp3.HeaderLen)
		if err != nil {
			return nil, waxmp3.Header{}, err
		}
		h, err := waxmp3.ParseHeader(candidate)
		if err != nil || h.Size() < waxmp3.HeaderLen || h.Size() > maxFrameSize {
			_, _ = f.r.ReadByte()
			continue
		}

		lookahead, lookErr := f.r.Peek(h.Size() + waxmp3.HeaderLen)
		if lookErr == nil {
			next, nextErr := waxmp3.ParseHeader(lookahead[h.Size():])
			if nextErr != nil || !h.Kin(next) {
				_, _ = f.r.ReadByte()
				continue
			}
		} else if len(lookahead) < h.Size() {
			return nil, waxmp3.Header{}, io.ErrUnexpectedEOF
		}
		frame := make([]byte, h.Size())
		if _, err := io.ReadFull(f.r, frame); err != nil {
			return nil, waxmp3.Header{}, err
		}
		return frame, h, nil
	}
}
