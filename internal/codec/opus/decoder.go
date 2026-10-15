// Package opus decodes Ogg Opus radio streams to signed 16-bit PCM.
package opus

import (
	"bytes"
	"fmt"
	"io"
	"math"

	"github.com/colespringer/waxflow/audio"
	waxopus "github.com/colespringer/waxflow/codec/opus"
	"github.com/pion/opus/pkg/oggreader"
	"go.lucor.dev/fynetune/internal/codec/pcm16"
	"go.lucor.dev/fynetune/internal/retry"
)

// Decoder reads sequential Ogg packets and emits interleaved PCM at 48 kHz.
type Decoder struct {
	packets  *oggreader.OggReader
	decoder  *waxopus.Decoder
	channels int
	skip     int
	decoded  uint64
	pcm      []byte
	offset   int
	err      error
}

// NewDecoder opens an Ogg Opus stream without seeking or buffering the full stream.
func NewDecoder(reader io.Reader) (*Decoder, error) {
	packets, head, err := oggreader.NewWith(reader)
	if err != nil {
		return nil, fmt.Errorf("opus: read Ogg identification header: %w", err)
	}
	if head.Version&0xf0 != 0 || head.ChannelMap != 0 || head.Channels < 1 || head.Channels > 2 {
		return nil, fmt.Errorf("opus: unsupported header version %d, mapping %d, channels %d", head.Version, head.ChannelMap, head.Channels)
	}
	cfg := waxopus.Config{Channels: int(head.Channels), PreSkip: int(head.PreSkip), Gain: int16(head.OutputGain)}
	decoder, err := waxopus.NewDecoder(cfg, cfg.Format())
	if err != nil {
		return nil, err
	}
	d := &Decoder{packets: packets, decoder: decoder, channels: cfg.Channels, skip: cfg.PreSkip}
	if err := d.readTags(); err != nil {
		decoder.Release()
		return nil, err
	}
	return d, nil
}

// readTags consumes the mandatory comment packet after an identification header.
func (d *Decoder) readTags() error {
	packet, _, err := d.packets.ParseNextPacket()
	if err != nil {
		return fmt.Errorf("opus: read comment header: %w", err)
	}
	if !bytes.HasPrefix(packet, []byte("OpusTags")) {
		return fmt.Errorf("opus: missing OpusTags header")
	}
	return nil
}

// Read decodes complete packets, honoring pre-skip and page granule end trimming.
func (d *Decoder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for d.offset == len(d.pcm) {
		if d.err != nil {
			return 0, d.err
		}
		if err := d.fill(); err != nil {
			d.err = err
			return 0, err
		}
	}
	n := copy(p, d.pcm[d.offset:])
	d.offset += n
	return n, nil
}

// fill decodes the next audio packet, or starts the next logical Opus stream.
func (d *Decoder) fill() error {
	d.pcm = d.pcm[:0]
	d.offset = 0
	packet, page, err := d.packets.ParseNextPacket()
	if err != nil {
		return err
	}
	if bytes.HasPrefix(packet, []byte("OpusHead")) {
		cfg, err := waxopus.ParseOpusHead(packet)
		if err != nil {
			return retry.NoRetry(err)
		}
		if cfg.Family != 0 || cfg.Channels != d.channels {
			return retry.NoRetry(fmt.Errorf("opus: chained stream changes channel layout"))
		}
		next, err := waxopus.NewDecoder(cfg, cfg.Format())
		if err != nil {
			return retry.NoRetry(err)
		}
		d.decoder.Release()
		d.decoder = next
		d.skip, d.decoded = cfg.PreSkip, 0
		return d.readTags()
	}
	if err := d.decoder.Decode(packet, func(buffer *audio.Buffer) error {
		d.pcm = pcm16.AppendS16LE(d.pcm, buffer)
		return nil
	}); err != nil {
		return retry.NoRetry(fmt.Errorf("opus: decode packet: %w", err))
	}
	frameBytes := d.channels * 2
	frames := len(d.pcm) / frameBytes
	d.decoded += uint64(frames)
	if page.GranulePosition != math.MaxUint64 && d.decoded > page.GranulePosition {
		trim := min(uint64(frames), d.decoded-page.GranulePosition)
		d.pcm = d.pcm[:len(d.pcm)-int(trim)*frameBytes]
	}
	skipped := min(d.skip, len(d.pcm)/frameBytes)
	d.skip -= skipped
	d.offset = skipped * frameBytes
	return nil
}

// SampleRate returns Opus's fixed decoded sample rate.
func (d *Decoder) SampleRate() int { return waxopus.SampleRate }

// Channels returns the number of interleaved PCM channels.
func (d *Decoder) Channels() int { return d.channels }

// Close releases decoder resources; the caller owns the input stream.
func (d *Decoder) Close() error {
	if d.decoder != nil {
		d.decoder.Release()
		d.decoder = nil
	}
	return nil
}
