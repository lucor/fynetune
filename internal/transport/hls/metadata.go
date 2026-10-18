package hls

import (
	"context"
	"log/slog"
	"sort"
	"sync"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/mpegts"
	"go.lucor.dev/fynetune/internal/id3"
	"go.lucor.dev/fynetune/internal/metadata"
)

const maxMetadataEvents = 64

// timedID3 retains source timestamps until the audio track establishes its clock.
type timedID3 struct {
	pts   int64
	tag   id3.Tag
	reset bool
}

// hlsMetadata belongs to one HLS attempt. Downloads populate a bounded queue;
// audio callbacks advance it. User callbacks run outside the lock and are
// suppressed after cancellation or Close.
type hlsMetadata struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	onMetadata  func(metadata.Event)
	queue       []timedID3
	raw         []timedID3
	clock       mpegts.TimeDecoder
	audioOrigin *int64
	videoOrigin *int64
	video       bool
	configured  bool
	clockReady  bool
	closed      bool
	previous    id3.Tag
	last        metadata.Track
}

func newHLSMetadata(ctx context.Context, onMetadata func(metadata.Event)) *hlsMetadata {
	if onMetadata == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	return &hlsMetadata{ctx: ctx, cancel: cancel, onMetadata: onMetadata}
}

// configure selects the leading clock used by gohlslib: H264 when present,
// otherwise the first audio track. Source timestamps use 90 kHz in MPEG-TS.
func (m *hlsMetadata) configure(video bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configured, m.video = true, video
	m.initializeClock()
}

func (m *hlsMetadata) initializeClock() {
	if !m.configured || m.clockReady {
		return
	}
	origin := m.audioOrigin
	if m.video {
		origin = m.videoOrigin
	}
	if origin == nil {
		return
	}
	m.clock.Initialize()
	m.clock.Decode(*origin)
	m.clockReady = true
	for _, item := range m.raw {
		m.enqueue(item)
	}
	m.raw = nil
}

// observe reads only already-downloaded bytes. Metadata failures never alter
// the bytes supplied to the audio demuxer or turn into playback failures.
func (m *hlsMetadata) observe(data []byte, discontinuity bool) {
	if m == nil {
		return
	}
	segment := scanMetadataSegment(m.ctx, data)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.ctx.Err() != nil {
		return
	}
	if m.audioOrigin == nil && segment.audioPTS != nil {
		v := *segment.audioPTS
		m.audioOrigin = &v
	}
	if m.videoOrigin == nil && segment.videoDTS != nil {
		v := *segment.videoDTS
		m.videoOrigin = &v
	}
	m.initializeClock()
	anchor := segment.audioPTS
	if m.video {
		anchor = segment.videoDTS
	}
	if m.clockReady && anchor != nil {
		m.clock.Decode(*anchor)
	}
	if discontinuity && anchor != nil {
		m.add(timedID3{pts: *anchor, reset: true})
	}
	for _, item := range segment.tags {
		m.add(item)
	}
}

func (m *hlsMetadata) add(item timedID3) {
	if !m.clockReady {
		if len(m.raw) < maxMetadataEvents {
			m.raw = append(m.raw, item)
		}
		return
	}
	m.enqueue(item)
}

func (m *hlsMetadata) enqueue(item timedID3) {
	item.pts = max(0, m.clock.Decode(item.pts))
	if len(m.queue) == maxMetadataEvents {
		// Retain the newest bounded set if a producer exceeds audio backpressure.
		m.queue = append(m.queue[:0], m.queue[1:]...)
	}
	m.queue = append(m.queue, item)
	sort.SliceStable(m.queue, func(i, j int) bool { return m.queue[i].pts < m.queue[j].pts })
}

// advance emits snapshots at the audio read position, rather than at download
// completion. It follows the same read-ahead behavior as the ICY reader.
func (m *hlsMetadata) advance(pts int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	var events []metadata.Event
	for len(m.queue) > 0 && m.queue[0].pts <= pts {
		item := m.queue[0]
		m.queue[0] = timedID3{}
		m.queue = m.queue[1:]
		if item.reset {
			m.previous = id3.Tag{}
			if m.last != (metadata.Track{}) {
				m.last = metadata.Track{}
				events = append(events, metadata.Event{Kind: metadata.Clear, Source: metadata.SourceID3})
			}
			continue
		}
		if len(item.tag.TextFrames) == 0 && len(item.tag.UserTextFrames) == 0 {
			continue // Transport timestamps and other non-text tags are not songs.
		}
		tag := item.tag
		if tag.IsUpdate {
			tag = mergeID3(m.previous, tag)
		}
		track := tag.Track()
		if track.String() == "" {
			continue
		}
		m.previous = tag
		if m.last.Equal(track) {
			continue
		}
		m.last = track
		events = append(events, metadata.Event{Kind: metadata.Replace, Source: metadata.SourceID3, Track: track})
	}
	m.mu.Unlock()
	for _, event := range events {
		if m.ctx.Err() == nil {
			m.onMetadata(event)
		}
	}
}

func (m *hlsMetadata) close() {
	if m == nil {
		return
	}
	m.cancel()
	m.mu.Lock()
	m.closed = true
	m.queue, m.raw = nil, nil
	m.previous = id3.Tag{}
	m.mu.Unlock()
}

// mergeID3 applies the standard partial-update flag without mutating source maps.
func mergeID3(previous, next id3.Tag) id3.Tag {
	merge := func(a, b map[string][]string) map[string][]string {
		result := make(map[string][]string, len(a)+len(b))
		for key, values := range a {
			result[key] = values
		}
		for key, values := range b {
			result[key] = values
		}
		return result
	}
	next.TextFrames = merge(previous.TextFrames, next.TextFrames)
	next.UserTextFrames = merge(previous.UserTextFrames, next.UserTextFrames)
	return next
}

// parseID3Payload reads complete tags from a metadata PES or packed-audio prefix.
// A malformed tag is skipped without scanning binary audio for guessed tag starts.
func parseID3Payload(data []byte, pts int64) []timedID3 {
	var tags []timedID3
	for len(tags) < maxMetadataEvents && len(data) >= 3 && string(data[:3]) == "ID3" {
		tag, err := id3.Parse(data)
		if err != nil {
			slog.Debug("HLS ID3 tag ignored", "reason", err)
			break
		}
		tags = append(tags, timedID3{pts: pts, tag: tag})
		data = data[tag.Size:]
	}
	return tags
}
