// Package aac frames ADTS streams for WaxFlow's packet decoder.
package aac

import (
	"bufio"
	"fmt"
	"io"
)

const maxADTSFrameLength = 8191

type Frame struct {
	AccessUnit    []byte
	Profile       int
	SampleRate    int
	ChannelConfig int
	HeaderLength  int
}

type Framer struct{ reader *bufio.Reader }

func NewFramer(reader io.Reader) *Framer {
	return &Framer{reader: bufio.NewReaderSize(reader, maxADTSFrameLength+9)}
}

func (f *Framer) ReadFrame() (Frame, error) {
	for {
		header, err := f.reader.Peek(7)
		if err != nil {
			return Frame{}, err
		}
		frame, ok := parseADTSHeader(header)
		if !ok {
			_, _ = f.reader.ReadByte()
			continue
		}
		length := frameLength(header)
		candidate, lookErr := f.reader.Peek(length + 7)
		if lookErr == nil {
			next, valid := parseADTSHeader(candidate[length:])
			if !valid || !sameADTSConfig(frame, next) {
				_, _ = f.reader.ReadByte()
				continue
			}
		} else if len(candidate) < length {
			return Frame{}, fmt.Errorf("read ADTS frame: %w", io.ErrUnexpectedEOF)
		}
		packet := make([]byte, length)
		if _, err := io.ReadFull(f.reader, packet); err != nil {
			return Frame{}, fmt.Errorf("read ADTS frame: %w", err)
		}
		frame.AccessUnit = packet[frame.HeaderLength:]
		return frame, nil
	}
}

func parseADTSHeader(b []byte) (Frame, bool) {
	if len(b) < 7 || b[0] != 0xff || b[1]&0xf6 != 0xf0 {
		return Frame{}, false
	}
	headerLen := 7
	if b[1]&1 == 0 {
		headerLen = 9
	}
	profile := int(b[2] >> 6)
	rateIndex := int(b[2] >> 2 & 0x0f)
	if rateIndex >= len(adtsSampleRates) || adtsSampleRates[rateIndex] == 0 {
		return Frame{}, false
	}
	channels := int(b[2]&1)<<2 | int(b[3]>>6)
	length := frameLength(b)
	if channels == 0 || length < headerLen || length > maxADTSFrameLength {
		return Frame{}, false
	}
	return Frame{Profile: profile, SampleRate: adtsSampleRates[rateIndex], ChannelConfig: channels, HeaderLength: headerLen}, true
}

func frameLength(b []byte) int {
	return int(b[3]&3)<<11 | int(b[4])<<3 | int(b[5]>>5)
}

func sameADTSConfig(a, b Frame) bool {
	return a.Profile == b.Profile && a.SampleRate == b.SampleRate && a.ChannelConfig == b.ChannelConfig
}

var adtsSampleRates = [...]int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
