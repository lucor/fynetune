package aac

import (
	"fmt"
	"io"

	"github.com/colespringer/waxflow/audio"
	waxaac "github.com/colespringer/waxflow/codec/aac"
	"go.lucor.dev/fynetune/internal/codec/pcm16"
	"go.lucor.dev/fynetune/internal/retry"
)

// maxProbeAccessUnits bounds the read-ahead used to detect AAC SBR and PS.
const maxProbeAccessUnits = 12

// NewDecoder frames ADTS access units and uses WaxFlow for AAC decoding.
func NewDecoder(reader io.Reader) (*Decoder, error) {
	if reader == nil {
		return nil, fmt.Errorf("aac: nil input reader")
	}
	frames := NewFramer(reader)
	first, err := frames.ReadFrame()
	if err != nil {
		return nil, fmt.Errorf("aac: read initial ADTS frame: %w", err)
	}
	if first.Profile != 1 {
		return nil, retry.NoRetry(fmt.Errorf("aac: ADTS profile %d is unsupported; AAC-LC is required", first.Profile))
	}
	asc, err := audioSpecificConfig(first.SampleRate, first.ChannelConfig)
	if err != nil {
		return nil, retry.NoRetry(err)
	}
	cfg, err := waxaac.ParseASC(asc)
	if err != nil {
		return nil, retry.NoRetry(fmt.Errorf("aac: parse ADTS configuration: %w", err))
	}
	units := [][]byte{first.AccessUnit}
	var probeErr error
	// SBR/PS detection needs several access units. Keep each probed unit so the
	// decoder can replay them instead of dropping audio consumed during probing.
	sbr, ps, err := waxaac.DetectSBR(cfg, func() []byte {
		if len(units) >= maxProbeAccessUnits {
			return nil
		}
		frame, readErr := frames.ReadFrame()
		if readErr != nil {
			probeErr = readErr
			return nil
		}
		if frame.Profile != first.Profile || frame.SampleRate != first.SampleRate || frame.ChannelConfig != first.ChannelConfig {
			probeErr = fmt.Errorf("ADTS stream configuration changed during SBR probe")
			return nil
		}
		units = append(units, frame.AccessUnit)
		return frame.AccessUnit
	})
	if err != nil {
		return nil, retry.NoRetry(fmt.Errorf("aac: detect SBR/PS: %w", err))
	}
	if probeErr != nil && probeErr != io.EOF {
		return nil, fmt.Errorf("aac: read ADTS probe frame: %w", probeErr)
	}
	if sbr {
		cfg.SBR = true
		cfg.PS = ps
		cfg.ExtensionRate = 2 * cfg.SampleRate
	}
	format, err := cfg.Format()
	if err != nil {
		return nil, retry.NoRetry(fmt.Errorf("aac: resolve PCM format: %w", err))
	}
	decoder, err := waxaac.NewDecoder(cfg, format)
	if err != nil {
		return nil, retry.NoRetry(fmt.Errorf("aac: create WaxFlow decoder: %w", err))
	}
	return &Decoder{frames: frames, decoder: decoder, config: cfg, format: format, pending: units}, nil
}

// Decoder converts ADTS AAC access units into interleaved signed 16-bit PCM.
type Decoder struct {
	frames  *Framer
	decoder *waxaac.Decoder
	config  waxaac.Config
	format  audio.Format
	pending [][]byte
	pcm     []byte
	offset  int
	err     error
}

// Read decodes AAC access units and returns their PCM bytes.
func (d *Decoder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for d.offset == len(d.pcm) {
		if d.err != nil {
			return 0, d.err
		}
		var unit []byte
		if len(d.pending) > 0 {
			unit = d.pending[0]
			d.pending[0] = nil
			d.pending = d.pending[1:]
		} else {
			frame, err := d.frames.ReadFrame()
			if err != nil {
				d.err = err
				return 0, err
			}
			if frame.Profile != 1 || frame.SampleRate != d.config.SampleRate || frame.ChannelConfig != d.config.ChannelConfig {
				return 0, retry.NoRetry(fmt.Errorf("aac: ADTS stream configuration changed"))
			}
			unit = frame.AccessUnit
		}
		d.pcm = d.pcm[:0]
		d.offset = 0
		if err := d.decoder.Decode(unit, func(b *audio.Buffer) error {
			d.pcm = pcm16.AppendS16LE(d.pcm, b)
			return nil
		}); err != nil {
			return 0, retry.NoRetry(fmt.Errorf("aac: decode access unit: %w", err))
		}
	}
	n := copy(p, d.pcm[d.offset:])
	d.offset += n
	return n, nil
}

// SampleRate returns the decoded PCM sample rate in hertz.
func (d *Decoder) SampleRate() int { return d.format.Rate }

// Channels returns the number of channels in the decoded PCM.
func (d *Decoder) Channels() int { return d.format.Channels }

// Close releases the underlying WaxFlow decoder.
func (d *Decoder) Close() error { d.decoder.Release(); return nil }

// audioSpecificConfig encodes an AAC-LC configuration derived from an ADTS header.
func audioSpecificConfig(rate, channelConfig int) ([]byte, error) {
	index := -1
	for i, sampleRate := range adtsSampleRates {
		if sampleRate == rate {
			index = i
			break
		}
	}
	if index < 0 || channelConfig < 1 || channelConfig > 7 {
		return nil, fmt.Errorf("aac: unsupported ADTS config: %d Hz, channel config %d", rate, channelConfig)
	}
	// AudioSpecificConfig: AAC-LC object type (2), sampling index, channel config.
	return []byte{byte(2<<3 | index>>1), byte(index&1<<7 | channelConfig<<3)}, nil
}
