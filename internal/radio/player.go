package radio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"go.lucor.dev/fynetune/internal/audio"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/media"
)

type PlayerState int

const (
	StateStopped PlayerState = iota
	StateConnecting
	StateBuffering
	StatePlaying
	StateReconnecting
	StateError
)

type TrackMetadata = media.TrackMetadata
type StreamInfo = media.StreamInfo

func ParseTrackMetadata(raw string) TrackMetadata { return media.ParseTrackMetadata(raw) }

type Event struct {
	State      PlayerState
	Station    Station
	Metadata   TrackMetadata
	StreamInfo StreamInfo
	Err        error
	Attempt    int
}

type Player interface {
	Play(Station)
	Stop()
	SetVolume(float64)
	SetReconnect(bool)
	State() PlayerState
	Events() <-chan Event
	Close() error
}

type StreamOpener interface {
	Open(context.Context, string, func(string)) (io.ReadCloser, media.StreamInfo, error)
}

type Engine struct {
	lifecycle        sync.Mutex
	mu               sync.Mutex
	session          uint64
	cancel           context.CancelFunc
	state            PlayerState
	volume           float64
	reconnectEnabled bool
	events           chan Event
	streams          StreamOpener
	decoders         codec.Factory
	output           audio.Output
	active           audio.Playback
	metadataParsers  map[string]*media.TrackMetadataParser
	closed           bool
}

func NewPlayer(streams StreamOpener, decoders codec.Factory, output audio.Output) *Engine {
	return &Engine{
		volume: .75, reconnectEnabled: true, events: make(chan Event, 32), streams: streams,
		decoders: decoders, output: output, metadataParsers: make(map[string]*media.TrackMetadataParser),
	}
}

func (p *Engine) parseMetadataLocked(station Station, raw string) TrackMetadata {
	key := ""
	switch {
	case station.ID != "":
		key = "id:" + station.ID
	case station.URL != "":
		key = "url:" + station.URL
	default:
		key = "name:" + station.Name
	}
	parser := p.metadataParsers[key]
	if parser == nil {
		parser = media.NewTrackMetadataParser()
		p.metadataParsers[key] = parser
	}
	return parser.Parse(raw)
}

func (p *Engine) parseMetadata(station Station, raw string) TrackMetadata {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.parseMetadataLocked(station, raw)
}

func (p *Engine) Events() <-chan Event { return p.events }

func (p *Engine) SetReconnect(enabled bool) {
	p.mu.Lock()
	p.reconnectEnabled = enabled
	p.mu.Unlock()
}

func (p *Engine) State() PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Engine) emit(event Event) {
	select {
	case p.events <- event:
	default:
	}
}

func (p *Engine) setState(ctx context.Context, session uint64, state PlayerState, station Station, err error, attempt int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != session || ctx.Err() != nil {
		return false
	}
	p.state = state
	p.emit(Event{State: state, Station: station, Err: err, Attempt: attempt})
	return true
}

func (p *Engine) SetVolume(volume float64) {
	if volume < 0 {
		volume = 0
	} else if volume > 1 {
		volume = 1
	}
	p.mu.Lock()
	p.volume = volume
	if p.active != nil {
		p.active.SetVolume(volume)
	}
	p.mu.Unlock()
}

func (p *Engine) Stop() {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	p.stopSession()
}

func (p *Engine) stopSession() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.session++
	p.cancel = nil
	playback := p.active
	p.active = nil
	p.state = StateStopped
	p.emit(Event{State: StateStopped})
	p.mu.Unlock()
	if playback != nil {
		_ = playback.Close()
	}
}

func (p *Engine) Play(station Station) {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	if p.closed {
		return
	}
	p.stopSession()
	ctx, cancel := context.WithCancel(context.Background())
	p.mu.Lock()
	p.cancel = cancel
	session := p.session
	p.mu.Unlock()
	go p.run(ctx, session, station)
}

func (p *Engine) run(ctx context.Context, session uint64, station Station) {
	attempt := 0
	for ctx.Err() == nil {
		state := StateConnecting
		if attempt > 0 {
			state = StateReconnecting
		}
		if !p.setState(ctx, session, state, station, nil, attempt) {
			return
		}
		connectedAt := time.Now()
		err := p.connect(ctx, session, station)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("radio stream ended")
		}
		p.mu.Lock()
		retry := p.reconnectEnabled
		p.mu.Unlock()
		if !retry {
			p.setState(ctx, session, StateError, station, err, attempt)
			return
		}
		if time.Since(connectedAt) >= 30*time.Second {
			attempt = 0
		}
		attempt++
		if !p.setState(ctx, session, StateReconnecting, station, err, attempt) {
			return
		}
		timer := time.NewTimer(RetryDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (p *Engine) connect(ctx context.Context, session uint64, station Station) error {
	streamURL := station.URL
	if station.ResolvedURL != "" {
		streamURL = station.ResolvedURL
	}
	var info StreamInfo
	stream, info, err := p.streams.Open(ctx, streamURL, func(raw string) {
		p.mu.Lock()
		if p.session != session || ctx.Err() != nil {
			p.mu.Unlock()
			return
		}
		metadata := p.parseMetadataLocked(station, raw)
		if metadata.RawTitle == "" {
			p.mu.Unlock()
			return
		}
		p.emit(Event{State: StatePlaying, Station: station, Metadata: metadata, StreamInfo: info})
		p.mu.Unlock()
		slog.Info("ICY StreamTitle received", "station", station.Name, "station_id", station.ID, "raw_title", metadata.RawTitle, "artist", metadata.Artist, "title", metadata.Title, "album", metadata.Album, "year", metadata.Year, "parsed", metadata.Parsed)
	})
	if err != nil {
		return err
	}
	defer stream.Close()
	if !p.setState(ctx, session, StateBuffering, station, nil, 0) {
		return ctx.Err()
	}
	decoded, err := p.decoders.New(stream)
	if err != nil {
		return fmt.Errorf("unable to decode %s stream: %w", info.Codec, err)
	}
	defer decoded.Close()
	if decoded.SampleRate() <= 0 || decoded.Channels() <= 0 {
		return errors.New("stream decoder returned an invalid PCM format")
	}
	p.mu.Lock()
	volume := p.volume
	p.mu.Unlock()
	playback, err := p.output.Play(ctx, decoded, audio.PCMFormat{SampleRate: decoded.SampleRate(), Channels: decoded.Channels(), BitDepth: 16}, volume)
	if err != nil {
		return err
	}
	defer func() {
		p.clearPlayback(playback)
		_ = playback.Close()
	}()
	p.mu.Lock()
	if p.session != session || ctx.Err() != nil {
		p.mu.Unlock()
		return ctx.Err()
	}
	p.active = playback
	p.mu.Unlock()
	p.mu.Lock()
	if p.session != session || ctx.Err() != nil {
		p.mu.Unlock()
		return ctx.Err()
	}
	p.state = StatePlaying
	p.emit(Event{State: StatePlaying, Station: station, StreamInfo: info})
	p.mu.Unlock()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := playback.Err(); err != nil {
				return fmt.Errorf("audio playback failed: %w", err)
			}
			if !playback.IsPlaying() {
				return io.EOF
			}
		}
	}
}

func (p *Engine) clearPlayback(playback audio.Playback) {
	p.mu.Lock()
	if p.active == playback {
		p.active = nil
	}
	p.mu.Unlock()
}

func (p *Engine) Close() error {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	p.stopSession()
	if closer, ok := p.streams.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	return nil
}
