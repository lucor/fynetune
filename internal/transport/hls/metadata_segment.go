package hls

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"sort"

	"github.com/asticode/go-astits"
	"go.lucor.dev/fynetune/internal/id3"
)

type metadataAudioPoint struct {
	pts    int64
	offset int
}

type metadataSegment struct {
	audioPTS *int64
	videoDTS *int64
	points   []metadataAudioPoint
	tags     []timedID3
}

// scanMetadataSegment demuxes plaintext MPEG-TS metadata or a packed ID3 prefix.
// It never changes the segment. Incomplete tags are discarded at a segment
// boundary, preventing malformed fragments from leaking into the next segment.
func scanMetadataSegment(ctx context.Context, data []byte) metadataSegment {
	if !isMPEGTS(data) {
		return metadataSegment{tags: parseID3Payload(data, 0)}
	}
	demuxer := astits.NewDemuxer(ctx, bytes.NewReader(data), astits.DemuxerOptPacketSize(mpegtsPacketSize),
		astits.DemuxerOptLogger(log.New(io.Discard, "", 0)))
	var result metadataSegment
	var audioPID, videoPID uint16
	var hasAudio, hasVideo bool
	metadataPIDs := make(map[uint16]bool)
	fragments := make(map[uint16][]byte)
	fragmentPTS := make(map[uint16]int64)
	audioBytes := 0
	for {
		packet, err := demuxer.NextData()
		if errors.Is(err, astits.ErrNoMorePackets) {
			break
		}
		if err != nil {
			slog.Debug("HLS metadata demux stopped", "reason", err)
			break
		}
		if packet.PMT != nil {
			for _, stream := range packet.PMT.ElementaryStreams {
				switch stream.StreamType {
				case astits.StreamTypeADTS:
					if !hasAudio {
						audioPID, hasAudio = stream.ElementaryPID, true
					}
				case astits.StreamTypeH264Video:
					if !hasVideo {
						videoPID, hasVideo = stream.ElementaryPID, true
					}
				case astits.StreamTypeMetadata, astits.StreamTypePrivateData:
					if len(metadataPIDs) < 8 {
						metadataPIDs[stream.ElementaryPID] = true
					}
				}
			}
		}
		if packet.PES == nil || packet.PES.Header == nil || packet.PES.Header.OptionalHeader == nil {
			continue
		}
		header := packet.PES.Header.OptionalHeader
		if hasAudio && packet.PID == audioPID {
			if header.PTS != nil {
				pts := header.PTS.Base
				if result.audioPTS == nil {
					result.audioPTS = &pts
				}
				result.points = append(result.points, metadataAudioPoint{pts: pts, offset: audioBytes})
			}
			audioBytes += len(packet.PES.Data)
		}
		if hasVideo && packet.PID == videoPID && result.videoDTS == nil {
			timestamp := header.DTS
			if timestamp == nil {
				timestamp = header.PTS
			}
			if timestamp != nil {
				v := timestamp.Base
				result.videoDTS = &v
			}
		}
		if !metadataPIDs[packet.PID] || len(result.tags) >= maxMetadataEvents {
			continue
		}
		payload := packet.PES.Data
		if header.DataAlignmentIndicator || len(fragments[packet.PID]) == 0 {
			fragments[packet.PID] = nil
			if header.PTS == nil || !bytes.HasPrefix(payload, []byte("ID3")) {
				continue
			}
			fragmentPTS[packet.PID] = header.PTS.Base
		}
		if len(fragments[packet.PID])+len(payload) > id3.MaxTagSize {
			delete(fragments, packet.PID)
			continue
		}
		buffer := append(fragments[packet.PID], payload...)
		for len(buffer) >= 3 && bytes.HasPrefix(buffer, []byte("ID3")) && len(result.tags) < maxMetadataEvents {
			tag, err := id3.Parse(buffer)
			if errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			if err != nil {
				slog.Debug("HLS ID3 tag ignored", "reason", err)
				buffer = nil
				break
			}
			result.tags = append(result.tags, timedID3{pts: fragmentPTS[packet.PID], tag: tag})
			buffer = buffer[tag.Size:]
		}
		fragments[packet.PID] = buffer
	}
	return result
}

type metadataAtOffset struct {
	offset int
	item   timedID3
}

// writeMetadataAAC interleaves metadata callbacks with writes of the existing
// fallback's audio bytes. PES timestamps locate the nearest following ADTS
// frame; split or malformed frames fall back to their PES byte boundary.
func writeMetadataAAC(writer io.Writer, audio []byte, segment metadataSegment, m *hlsMetadata, discontinuity bool) error {
	if m == nil {
		_, err := writer.Write(audio)
		return err
	}
	var events []metadataAtOffset
	if discontinuity {
		events = append(events, metadataAtOffset{item: timedID3{reset: true}})
	}
	for _, tag := range segment.tags {
		offset := 0
		for _, point := range segment.points {
			delta := timestampDelta(tag.pts, point.pts)
			if delta < 0 {
				break
			}
			offset = metadataFrameOffset(audio, point.offset, delta)
		}
		events = append(events, metadataAtOffset{offset: offset, item: tag})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].offset < events[j].offset })
	position := 0
	for _, event := range events {
		if event.offset > position {
			if _, err := writer.Write(audio[position:event.offset]); err != nil {
				return err
			}
			position = event.offset
		}
		m.mu.Lock()
		if m.closed || m.ctx.Err() != nil {
			m.mu.Unlock()
			return m.ctx.Err()
		}
		m.queue = append(m.queue, event.item)
		m.queue[len(m.queue)-1].pts = 0
		m.mu.Unlock()
		m.advance(0)
	}
	_, err := writer.Write(audio[position:])
	return err
}

// timestampDelta compares transport timestamps across their 33-bit rollover.
func timestampDelta(a, b int64) int64 {
	const wrap = int64(1) << 33
	delta := (a - b) & (wrap - 1)
	if delta >= wrap/2 {
		delta -= wrap
	}
	return delta
}

// metadataFrameOffset locates the first complete frame at or after the timestamp.
func metadataFrameOffset(data []byte, offset int, delta int64) int {
	position, samples := offset, int64(0)
	for position+7 <= len(data) {
		h := data[position:]
		rateIndex := int(h[2] >> 2 & 15)
		length := int(h[3]&3)<<11 | int(h[4])<<3 | int(h[5]>>5)
		if h[0] != 0xff || h[1]&0xf6 != 0xf0 || rateIndex >= len(adtsSampleRates) || length < 7 || length > len(h) {
			return offset
		}
		if samples*90000/int64(adtsSampleRates[rateIndex]) >= delta {
			return position
		}
		samples += 1024 * int64(1+(h[6]&3))
		position += length
	}
	return len(data)
}
