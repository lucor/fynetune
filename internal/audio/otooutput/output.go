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
	"go.lucor.dev/fynetune/internal/retry"
)

var (
	// ErrUnsupportedPCMFormat indicates that the output cannot play the requested PCM format.
	ErrUnsupportedPCMFormat = errors.New("unsupported PCM format")
	// ErrOutputUnavailable indicates that Oto could not initialize or use the audio device.
	ErrOutputUnavailable = errors.New("audio output unavailable")
	// ErrInvalidSampleRate indicates that a resampler was given a non-positive sample rate.
	ErrInvalidSampleRate = errors.New("invalid PCM sample rate")
)

// outputSampleRate is the shared hardware rate used by the Oto context.
const outputSampleRate = 44100

// pcmFrameSize is the byte size of one stereo frame of signed 16-bit PCM.
const pcmFrameSize = 4

// Output initializes Oto once and adapts input PCM to its fixed stereo format.
type Output struct {
	once    sync.Once // Oto context creation is shared by every playback from this output.
	context *oto.Context
	ready   chan struct{}
	err     error
}

// New creates an output that initializes its Oto context on the first Play call.
func New() *Output { return &Output{} }

// Play validates and adapts PCM input, then starts an Oto playback session.
func (o *Output) Play(ctx context.Context, source io.Reader, format audio.PCMFormat, volume float64) (audio.Playback, error) {
	if format.SampleRate <= 0 || (format.Channels != 1 && format.Channels != 2) || format.BitDepth != 16 {
		return nil, retry.NoRetry(fmt.Errorf("%w: %d Hz, %d channels, %d-bit", ErrUnsupportedPCMFormat, format.SampleRate, format.Channels, format.BitDepth))
	}
	o.once.Do(func() {
		o.context, o.ready, o.err = oto.NewContext(&oto.NewContextOptions{
			SampleRate: outputSampleRate, ChannelCount: 2, Format: oto.FormatSignedInt16LE,
			BufferSize: 100 * time.Millisecond,
		})
	})
	if o.err != nil {
		return nil, retry.NoRetry(fmt.Errorf("%w: %w", ErrOutputUnavailable, o.err))
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
		return nil, retry.NoRetry(fmt.Errorf("%w: %w", ErrOutputUnavailable, err))
	}
	if format.Channels == 1 {
		source = &monoToStereo{source: source}
	}
	if format.SampleRate != outputSampleRate {
		source = &resampler{source: source, inputRate: format.SampleRate, outputRate: outputSampleRate}
	}
	otoPlayer := o.context.NewPlayer(source)
	otoPlayer.SetVolume(volume)
	otoPlayer.Play()
	return &otoPlayback{otoPlayer: otoPlayer}, nil
}

// otoPlayback adapts an Oto player to the application playback contract.
type otoPlayback struct {
	otoPlayer *oto.Player
	closeOnce sync.Once
}

// SetVolume updates the Oto player's volume.
func (p *otoPlayback) SetVolume(value float64) { p.otoPlayer.SetVolume(value) }

// Err returns the Oto player's asynchronous error, if any.
func (p *otoPlayback) Err() error { return p.otoPlayer.Err() }

// IsPlaying reports whether the Oto player is active.
func (p *otoPlayback) IsPlaying() bool { return p.otoPlayer.IsPlaying() }

// Close stops the player once, safely coordinating with the session's deferred cleanup.
func (p *otoPlayback) Close() error {
	p.closeOnce.Do(p.otoPlayer.PauseAndStopReading)
	return nil
}

// stereoFrame stores one interleaved stereo sample pair.
type stereoFrame struct{ left, right int16 }

// monoToStereo duplicates each mono sample into Oto's stereo output.
type monoToStereo struct {
	source io.Reader
	sample [2]byte
}

// Read duplicates each signed 16-bit mono sample into a stereo frame.
func (r *monoToStereo) Read(dst []byte) (int, error) {
	// Oto consumes whole stereo frames, so ignore any trailing partial frame space.
	dst = dst[:len(dst)/pcmFrameSize*pcmFrameSize]
	if len(dst) == 0 {
		return 0, nil
	}
	written := 0
	for written < len(dst) {
		if _, err := io.ReadFull(r.source, r.sample[:]); err != nil {
			if written > 0 {
				return written, nil
			}
			return 0, err
		}
		copy(dst[written:written+2], r.sample[:])
		copy(dst[written+2:written+4], r.sample[:])
		written += pcmFrameSize
	}
	return written, nil
}

// resampler linearly interpolates signed 16-bit stereo PCM to Oto's shared output rate.
type resampler struct {
	source            io.Reader
	inputRate         int
	outputRate        int
	phase             int64
	left, right       stereoFrame
	initialized, done bool
	frameBytes        [pcmFrameSize]byte
}

// Read resamples complete stereo frames and keeps the output frame-aligned.
func (r *resampler) Read(dst []byte) (int, error) {
	// Keep output frame-aligned even when the caller supplies an odd buffer length.
	dst = dst[:len(dst)/pcmFrameSize*pcmFrameSize]
	if len(dst) == 0 {
		return 0, nil
	}
	if r.inputRate <= 0 || r.outputRate <= 0 {
		return 0, retry.NoRetry(ErrInvalidSampleRate)
	}
	if !r.initialized {
		// Interpolation needs samples on both sides of the first output position.
		left, err := r.readFrame()
		if err != nil {
			return 0, err
		}
		right, err := r.readFrame()
		if err != nil {
			return 0, err
		}
		r.left, r.right, r.initialized = left, right, true
	}
	written := 0
	// phase is a fractional position between left and right, scaled by outputRate.
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
			next, err := r.readFrame()
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

// readFrame reads one complete stereo PCM frame from the source.
func (r *resampler) readFrame() (stereoFrame, error) {
	// Reuse the frame bytes: a local array escapes through io.Reader on every
	// sample, allocating tens of thousands of tiny buffers per second.
	b := r.frameBytes[:]
	if _, err := io.ReadFull(r.source, b); err != nil {
		return stereoFrame{}, err
	}
	return stereoFrame{left: int16(binary.LittleEndian.Uint16(b[:2])), right: int16(binary.LittleEndian.Uint16(b[2:]))}, nil
}
