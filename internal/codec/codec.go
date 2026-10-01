// Package codec defines audio decoder contracts and selection boundaries.
package codec

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"go.lucor.dev/fynetune/internal/codec/mp3"
	"go.lucor.dev/fynetune/internal/media"
)

var (
	ErrNilRegistry              = errors.New("codec registry is nil")
	ErrEmptyCodecName           = errors.New("codec name is required")
	ErrNilDecoderConstructor    = errors.New("decoder constructor is required")
	ErrDecoderAlreadyRegistered = errors.New("decoder is already registered")
	ErrUnsupportedCodec         = errors.New("unsupported audio codec")
)

type Decoder interface {
	io.Reader
	SampleRate() int
	Channels() int
	Close() error
}

// Registry selects a decoder based on the codec identified for a stream.
type Registry struct {
	decoders map[string]func(io.Reader) (Decoder, error)
}

// New creates a decoder registry populated with the supported codecs.
func New() *Registry {
	registry := NewRegistry()
	registry.Register("MP3", func(reader io.Reader) (Decoder, error) {
		return mp3.NewDecoder(reader)
	})
	return registry
}

// NewRegistry creates an empty registry. Use it when supplying decoders
// explicitly, such as in tests or a custom application configuration.
func NewRegistry() *Registry {
	return &Registry{decoders: make(map[string]func(io.Reader) (Decoder, error))}
}

// Register adds a decoder for a codec name. Codec names are matched without
// regard to case or surrounding whitespace.
func (r *Registry) Register(name string, newDecoder func(io.Reader) (Decoder, error)) error {
	if r == nil {
		return ErrNilRegistry
	}
	name = normalize(name)
	if name == "" {
		return ErrEmptyCodecName
	}
	if newDecoder == nil {
		return ErrNilDecoderConstructor
	}
	if _, ok := r.decoders[name]; ok {
		return fmt.Errorf("%w: %s", ErrDecoderAlreadyRegistered, name)
	}
	r.decoders[name] = newDecoder
	return nil
}

// Decode creates the decoder registered for the stream's codec.
func (r *Registry) Decode(reader io.Reader, info media.StreamInfo) (Decoder, error) {
	if r == nil {
		return nil, ErrNilRegistry
	}
	name := normalize(info.Codec)
	newDecoder, ok := r.decoders[name]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnsupportedCodec, info.Codec)
	}
	return newDecoder(reader)
}

func normalize(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}
