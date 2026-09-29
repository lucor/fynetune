// Package mp3 adapts go-mp3 to the application's decoder interface.
package mp3

import (
	"io"

	"github.com/hajimehoshi/go-mp3"
	"go.lucor.dev/fynetune/internal/codec"
)

type Factory struct{}

func (Factory) New(reader io.Reader) (codec.Decoder, error) {
	decoded, err := mp3.NewDecoder(reader)
	if err != nil {
		return Decoder{}, err
	}
	return Decoder{decoded: decoded}, nil
}

type Decoder struct{ decoded *mp3.Decoder }

func (d Decoder) Read(p []byte) (int, error) { return d.decoded.Read(p) }
func (d Decoder) SampleRate() int            { return d.decoded.SampleRate() }
func (Decoder) Channels() int                { return 2 }
func (Decoder) Close() error                 { return nil }
