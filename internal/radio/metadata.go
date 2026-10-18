package radio

import (
	"context"
	"log/slog"

	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/metadata"
)

// attemptMetadata belongs to one connection attempt. Engine.mu protects its
// mutable fields, including metadata received before audio playback starts.
type attemptMetadata struct {
	player  *Engine
	ctx     context.Context
	session uint64
	station Station
	track   metadata.Track
	info    codec.StreamInfo
	playing bool
}

func (m *attemptMetadata) apply(event metadata.Event) {
	p := m.player
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != m.session || m.ctx.Err() != nil {
		return
	}
	var next metadata.Track
	switch event.Kind {
	case metadata.Replace:
		if event.Track.String() == "" {
			return
		}
		next = event.Track
	case metadata.Clear:
	default:
		return
	}
	if m.track.Equal(next) {
		return
	}
	m.track = next
	if m.playing {
		p.emit(Event{State: StatePlaying, Station: m.station, Metadata: next, StreamInfo: m.info})
	}
	slog.Debug("stream metadata applied", "station", m.station.Name, "station_id", m.station.ID,
		"source", event.Source, "kind", event.Kind)
}
