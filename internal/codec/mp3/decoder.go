package mp3

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/colespringer/waxflow/audio"
	waxmp3 "github.com/colespringer/waxflow/codec/mp3"
	"go.lucor.dev/fynetune/internal/retry"
)

// NewDecoder frames an elementary MP3 stream and feeds complete packets to WaxFlow.
func NewDecoder(reader io.Reader) (*Decoder, error) {
	if reader == nil {
		return nil, fmt.Errorf("mp3: nil input reader")
	}
	frames := NewFramer(reader)
	frame, header, err := frames.ReadFrame()
	if err != nil {
		return nil, fmt.Errorf("mp3: read initial frame: %w", err)
	}
	decoded, err := waxmp3.NewDecoder(header.PCMFormat())
	if err != nil {
		return nil, retry.Permanent(fmt.Errorf("mp3: create WaxFlow decoder: %w", err))
	}
	return &Decoder{frames: frames, decoder: decoded, format: header.PCMFormat(), pending: frame}, nil
}

type Decoder struct {
	frames  *Framer
	decoder *waxmp3.Decoder
	format  audio.Format
	pending []byte
	pcm     []byte
	offset  int
	err     error
}

func (d *Decoder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for d.offset == len(d.pcm) {
		if d.err != nil {
			return 0, d.err
		}
		frame := d.pending
		if frame != nil {
			d.pending = nil
		} else {
			var h waxmp3.Header
			var err error
			frame, h, err = d.frames.ReadFrame()
			if err != nil {
				d.err = err
				return 0, err
			}
			if h.PCMFormat() != d.format {
				return 0, retry.Permanent(fmt.Errorf("mp3: stream format changed from %v to %v", d.format, h.PCMFormat()))
			}
		}
		d.pcm = d.pcm[:0]
		d.offset = 0
		err := d.decoder.Decode(frame, func(b *audio.Buffer) error {
			size := b.N * b.Fmt.Channels * 2
			if cap(d.pcm) < size {
				d.pcm = make([]byte, size)
			} else {
				d.pcm = d.pcm[:size]
			}
			for i := 0; i < b.N; i++ {
				for c := 0; c < b.Fmt.Channels; c++ {
					v := b.ChanF(c)[i]
					if v > 1 {
						v = 1
					}
					if v < -1 {
						v = -1
					}
					sample := int16(v * 32767)
					binary.LittleEndian.PutUint16(d.pcm[(i*b.Fmt.Channels+c)*2:], uint16(sample))
				}
			}
			return nil
		})
		if err != nil {
			return 0, retry.Permanent(fmt.Errorf("mp3: decode frame: %w", err))
		}
	}
	n := copy(p, d.pcm[d.offset:])
	d.offset += n
	return n, nil
}
func (d *Decoder) SampleRate() int { return d.format.Rate }
func (d *Decoder) Channels() int   { return d.format.Channels }
func (d *Decoder) Close() error {
	if d.decoder != nil {
		d.decoder.Release()
	}
	return nil
}
