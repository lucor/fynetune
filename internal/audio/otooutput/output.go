// Package otooutput adapts decoded PCM streams to Oto.
package otooutput

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"go.lucor.dev/fynetune/internal/audio"
)

var (
	ErrUnsupportedPCMFormat = errors.New("unsupported PCM format")
	ErrOutputUnavailable    = errors.New("audio output unavailable")
	ErrInvalidSampleRate    = errors.New("invalid PCM sample rate")
)

const outputSampleRate = 44100
const pcmFrameSize = 4

type Output struct {
	once    sync.Once
	context *oto.Context
	ready   chan struct{}
	err     error
}

func New() *Output { return &Output{} }

func (o *Output) Play(ctx context.Context, source io.Reader, format audio.PCMFormat, volume float64) (audio.Playback, error) {
	if format.SampleRate <= 0 || format.Channels != 2 || format.BitDepth != 16 {
		return nil, fmt.Errorf("%w: %d Hz, %d channels, %d-bit", ErrUnsupportedPCMFormat, format.SampleRate, format.Channels, format.BitDepth)
	}
	o.once.Do(func() {
		o.context, o.ready, o.err = oto.NewContext(&oto.NewContextOptions{
			SampleRate: outputSampleRate, ChannelCount: 2, Format: oto.FormatSignedInt16LE,
			BufferSize: 100 * time.Millisecond,
		})
	})
	if o.err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOutputUnavailable, o.err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-o.ready:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := o.context.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOutputUnavailable, err)
	}
	if format.SampleRate != outputSampleRate {
		source = &resampler{source: source, inputRate: format.SampleRate, outputRate: outputSampleRate}
	}
	player := o.context.NewPlayer(source)
	player.SetVolume(volume)
	player.Play()
	return &otoPlayback{player: player}, nil
}

type otoPlayback struct {
	player    *oto.Player
	closeOnce sync.Once
}

func (p *otoPlayback) SetVolume(value float64) { p.player.SetVolume(value) }
func (p *otoPlayback) Err() error              { return p.player.Err() }
func (p *otoPlayback) IsPlaying() bool         { return p.player.IsPlaying() }

// Close can race between Stop and the session's deferred cleanup.
func (p *otoPlayback) Close() error {
	p.closeOnce.Do(p.player.PauseAndStopReading)
	return nil
}

type stereoFrame struct{ left, right int16 }

// resampler converts signed 16-bit stereo PCM to Oto's shared output rate.
type resampler struct {
	source            io.Reader
	inputRate         int
	outputRate        int
	phase             int64
	left, right       stereoFrame
	initialized, done bool
}

func (r *resampler) Read(dst []byte) (int, error) {
	dst = dst[:len(dst)/pcmFrameSize*pcmFrameSize]
	if len(dst) == 0 {
		return 0, nil
	}
	if r.inputRate <= 0 || r.outputRate <= 0 {
		return 0, ErrInvalidSampleRate
	}
	if !r.initialized {
		left, err := readStereoFrame(r.source)
		if err != nil {
			return 0, err
		}
		right, err := readStereoFrame(r.source)
		if err != nil {
			return 0, err
		}
		r.left, r.right, r.initialized = left, right, true
	}
	written := 0
	for written < len(dst) && !r.done {
		for channel, a := range [2]int16{r.left.left, r.left.right} {
			b := r.right.left
			if channel == 1 {
				b = r.right.right
			}
			value := int64(a) + (int64(b)-int64(a))*r.phase/int64(r.outputRate)
			binary.LittleEndian.PutUint16(dst[written+channel*2:], uint16(int16(value)))
		}
		written += pcmFrameSize
		r.phase += int64(r.inputRate)
		for r.phase >= int64(r.outputRate) {
			r.phase -= int64(r.outputRate)
			r.left = r.right
			next, err := readStereoFrame(r.source)
			if err != nil {
				r.done = true
				break
			}
			r.right = next
		}
	}
	if written == 0 {
		return 0, io.EOF
	}
	return written, nil
}

func readStereoFrame(source io.Reader) (stereoFrame, error) {
	var b [pcmFrameSize]byte
	if _, err := io.ReadFull(source, b[:]); err != nil {
		return stereoFrame{}, err
	}
	return stereoFrame{left: int16(binary.LittleEndian.Uint16(b[:2])), right: int16(binary.LittleEndian.Uint16(b[2:]))}, nil
}
