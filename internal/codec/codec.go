// Package codec defines audio decoder contracts and selection boundaries.
package codec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"strings"

	"go.lucor.dev/fynetune/internal/codec/aac"
	"go.lucor.dev/fynetune/internal/codec/mp3"
	"go.lucor.dev/fynetune/internal/retry"
)

// StreamInfo describes the source and codec of an opened audio stream.
type StreamInfo struct {
	URL      string    // URL of the resolved audio stream.
	Codec    CodecName // Codec selected for decoding.
	MIMEType string    // MIME type reported for the stream.
}

// CodecName identifies an audio codec supported by the decoder registry.
type CodecName string

// Names of built-in audio codecs used in stream information and by the decoder registry.
const (
	NameMP3  CodecName = "MP3"
	NameAAC  CodecName = "AAC"
	NameOGG  CodecName = "OGG"
	NameOPUS CodecName = "OPUS"
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
	decoders map[CodecName]func(io.Reader) (Decoder, error)
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
func NameFromMIMEType(contentType string) CodecName {
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
			return CodecName(strings.ToUpper(after))
		}
		return CodecName(strings.ToUpper(mediaType))
	}
}

// isADTSHeader recognizes the sync word and layer bits of an AAC ADTS header.
func isADTSHeader(prefix []byte) bool {
	return len(prefix) >= 2 && prefix[0] == 0xff && prefix[1]&0xf6 == 0xf0
}

// NewRegistry creates an empty registry. Use it when supplying decoders
// explicitly, such as in tests or a custom application configuration.
func NewRegistry() *Registry {
	return &Registry{decoders: make(map[CodecName]func(io.Reader) (Decoder, error))}
}

// Register adds a decoder for a codec name. Codec names are matched without
// regard to case or surrounding whitespace.
func (r *Registry) Register(name CodecName, newDecoder func(io.Reader) (Decoder, error)) error {
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

// Decode creates the decoder registered for the stream's codec and updates info
// when the audio header identifies a different codec. info must be non-nil.
func (r *Registry) Decode(reader io.Reader, info *StreamInfo) (Decoder, error) {
	if r == nil {
		return nil, ErrNilRegistry
	}
	if info.MIMEType != "" && info.Codec == NameMP3 {
		buffered := bufio.NewReader(reader)
		prefix, _ := buffered.Peek(2)
		// Some radio servers label AAC streams as audio/mpeg.
		if isADTSHeader(prefix) {
			info.Codec = NameAAC
		}
		reader = buffered
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
func normalize(name CodecName) CodecName {
	return CodecName(strings.ToUpper(strings.TrimSpace(string(name))))
}
