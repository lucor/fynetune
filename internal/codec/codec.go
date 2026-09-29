// Package codec defines the decoder contract used by the player service.
package codec

import "io"

type Decoder interface {
	io.Reader
	SampleRate() int
	Channels() int
	Close() error
}

type Factory interface {
	New(io.Reader) (Decoder, error)
}
