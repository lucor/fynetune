// Package audio defines the platform-independent PCM output contract.
package audio

import (
	"context"
	"io"
)

// PCMFormat describes the signed integer PCM format consumed by an Output.
type PCMFormat struct {
	SampleRate int // Samples per second.
	Channels   int // Number of interleaved audio channels.
	BitDepth   int // Bits per sample.
}

// Playback controls audio currently submitted to an Output.
type Playback interface {
	// SetVolume changes the playback volume.
	SetVolume(float64)
	// Err reports an asynchronous playback error, if one occurred.
	Err() error
	// IsPlaying reports whether playback is active.
	IsPlaying() bool
	// Close stops playback and releases its resources.
	Close() error
}

// Output plays a stream of PCM samples in the supplied format.
type Output interface {
	// Play starts playback and returns a handle for controlling the session.
	Play(context.Context, io.Reader, PCMFormat, float64) (Playback, error)
}
