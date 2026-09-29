// Package audio defines the platform-independent PCM output contract.
package audio

import (
	"context"
	"io"
)

type PCMFormat struct {
	SampleRate int
	Channels   int
	BitDepth   int
}

type Playback interface {
	SetVolume(float64)
	Err() error
	IsPlaying() bool
	Close() error
}

type Output interface {
	Play(context.Context, io.Reader, PCMFormat, float64) (Playback, error)
}
