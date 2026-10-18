package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astits"
	"go.lucor.dev/fynetune/internal/codec/aac"
	"go.lucor.dev/fynetune/internal/id3"
)

const (
	mpegtsSyncByte         = 0x47
	mpegtsPacketSize       = 188
	mpegtsDetectionSamples = 2
)

// extractSegmentAAC removes MPEG-TS and PES framing without assuming that
// individual PES payloads contain complete ADTS frames. The caller concatenates
// these bytes before the AAC codec frames them, including across HLS segments.
func extractSegmentAAC(ctx context.Context, data []byte) ([]byte, error) {
	if isMPEGTS(data) {
		return extractMPEGTSAAC(ctx, data)
	}
	// Packed-audio tags are framing, not AAC. Removing them also keeps the
	// previous segment's last ADTS frame adjacent to the next audio frame.
	for bytes.HasPrefix(data, []byte("ID3")) {
		tag, err := id3.Parse(data)
		if err != nil {
			break
		}
		data = data[tag.Size:]
	}
	if _, err := aac.NewFramer(bytes.NewReader(data)).ReadFrame(); err != nil {
		return nil, fmt.Errorf("%w: %v", errNotPackedAAC, err)
	}
	return data, nil
}

// isMPEGTS checks for the MPEG-TS sync byte at the start of two consecutive
// 188-byte packets. Checking both positions avoids mistaking an AAC payload
// that happens to contain 0x47 for a transport stream.
func isMPEGTS(data []byte) bool {
	minimumSize := mpegtsPacketSize * mpegtsDetectionSamples
	if len(data) < minimumSize {
		return false
	}
	for packet := 0; packet < mpegtsDetectionSamples; packet++ {
		if data[packet*mpegtsPacketSize] != mpegtsSyncByte {
			return false
		}
	}
	return true
}

// extractMPEGTSAAC demuxes one 188-byte-packet MPEG-TS segment and concatenates
// AAC PES payloads without requiring ADTS frames to end at PES boundaries.
func extractMPEGTSAAC(ctx context.Context, data []byte) ([]byte, error) {
	demuxer := astits.NewDemuxer(ctx, bytes.NewReader(data), astits.DemuxerOptPacketSize(mpegtsPacketSize))
	var audioPID uint16
	var selected bool
	var audio []byte
	for {
		packet, err := demuxer.NextData()
		if errors.Is(err, astits.ErrNoMorePackets) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("demux AAC segment: %w", err)
		}
		if packet.PMT != nil && !selected {
			for _, stream := range packet.PMT.ElementaryStreams {
				if stream.StreamType == astits.StreamTypeADTS {
					audioPID, selected = stream.ElementaryPID, true
					break
				}
			}
		}
		if selected && packet.PID == audioPID && packet.PES != nil {
			audio = append(audio, packet.PES.Data...)
		}
	}
	if !selected {
		return nil, fmt.Errorf("%w: %w", errNotPackedAAC, ErrNoAACTrack)
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("MPEG-TS segment contains no AAC payload")
	}
	return audio, nil
}
