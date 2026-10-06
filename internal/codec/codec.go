// Package codec defines audio decoder contracts and selection boundaries.
package codec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"strings"

	"go.lucor.dev/fynetune/internal/codec/aac"
	"go.lucor.dev/fynetune/internal/codec/mp3"
	"go.lucor.dev/fynetune/internal/media"
	"go.lucor.dev/fynetune/internal/retry"
)

// Codec names used in stream information and by the decoder registry.
const (
	NameMP3  = "MP3"
	NameAAC  = "AAC"
	NameOGG  = "OGG"
	NameOPUS = "OPUS"
)

var (
	// ErrNilRegistry indicates a method was called on a nil registry.
	ErrNilRegistry = errors.New("codec registry is nil")
	// ErrEmptyCodecName indicates registration was attempted without a name.
	ErrEmptyCodecName = errors.New("codec name is required")
	// ErrNilDecoderConstructor indicates registration was attempted without a constructor.
	ErrNilDecoderConstructor = errors.New("decoder constructor is required")
	// ErrDecoderAlreadyRegistered indicates that a codec name is already in use.
	ErrDecoderAlreadyRegistered = errors.New("decoder is already registered")
	// ErrUnsupportedCodec indicates that no decoder is registered for a stream codec.
	ErrUnsupportedCodec = errors.New("unsupported audio codec")
)

// Decoder reads decoded PCM and reports its format.
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
	registry.Register(NameMP3, func(reader io.Reader) (Decoder, error) {
		return mp3.NewDecoder(reader)
	})
	registry.Register(NameAAC, func(reader io.Reader) (Decoder, error) {
		return aac.NewDecoder(reader)
	})
	return registry
}

// NameFromMIMEType maps a stream media type to a decoder name. Missing or
// malformed types and generic octet-stream types default to MP3 for radio
// servers that omit a useful content type. Other unknown media types are
// returned in uppercase so the registry can report them as unsupported.
func NameFromMIMEType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" {
		return NameMP3
	}
	mediaType = strings.ToLower(mediaType)
	switch mediaType {
	case "audio/mpeg", "audio/mp3", "audio/x-mpeg":
		return NameMP3
	case "audio/aac", "audio/aacp", "audio/x-aac":
		return NameAAC
	case "audio/ogg", "application/ogg":
		return NameOGG
	case "audio/opus":
		return NameOPUS
	case "application/octet-stream", "application/x-octet-stream", "binary/octet-stream":
		return NameMP3
	default:
		if after, ok := strings.CutPrefix(mediaType, "audio/"); ok {
			return strings.ToUpper(after)
		}
		return strings.ToUpper(mediaType)
	}
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
		return nil, retry.NoRetry(fmt.Errorf("%w %q", ErrUnsupportedCodec, info.Codec))
	}
	decoder, err := newDecoder(reader)
	if err != nil {
		err = fmt.Errorf("initialize %s decoder: %w", name, err)
		var networkErr net.Error
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.As(err, &networkErr) {
			err = retry.NoRetry(err)
		}
		return nil, err
	}
	return decoder, nil
}

// normalize trims surrounding whitespace and uppercases a codec name.
func normalize(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}
