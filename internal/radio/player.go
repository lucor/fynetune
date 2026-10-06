// Package radio models stations and coordinates stream playback.
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
	"go.lucor.dev/fynetune/internal/icy"
	retrypolicy "go.lucor.dev/fynetune/internal/retry"
)

var (
	// ErrStreamEnded indicates that a stream ended without a playback error.
	ErrStreamEnded = errors.New("radio stream ended")
	// ErrInvalidPCMFormat indicates that a decoder returned an invalid audio format.
	ErrInvalidPCMFormat = errors.New("stream decoder returned an invalid PCM format")
	// ErrAudioPlaybackFailed indicates that the audio output failed during playback.
	ErrAudioPlaybackFailed = errors.New("audio playback failed")
)

// PlayerState is the current state of radio playback.
type PlayerState int

const (
	StateStopped      PlayerState = iota // No station is playing.
	StateConnecting                      // The player is opening a stream.
	StateBuffering                       // The stream is open and the decoder is starting.
	StatePlaying                         // Audio is playing.
	StateReconnecting                    // The player is waiting to retry a failed stream.
	StateError                           // Playback stopped after a non-retryable failure.
)

// Event reports a playback state change or a track metadata update.
type Event struct {
	State      PlayerState       // Playback state associated with this event.
	Station    Station           // Station associated with this event, when available.
	Metadata   icy.TrackMetadata // Track metadata; zero-valued for state-only events.
	StreamInfo codec.StreamInfo  // Resolved stream information, when available.
	Err        error             // Failure that caused a reconnect or stopped playback.
	Attempt    int               // Reconnect attempt count; zero before the first retry.
}

// Player controls station playback and exposes playback events to the UI.
type Player interface {
	Play(Station)
	Stop()
	SetVolume(float64)
	SetReconnect(bool)
	State() PlayerState
	Events() <-chan Event
	Close() error
}

// StreamOpener resolves a station URL and opens its audio stream.
type StreamOpener interface {
	Open(context.Context, string, func(string)) (io.ReadCloser, codec.StreamInfo, error)
}

// Engine coordinates stream resolution, decoding, audio output, and reconnects.
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
	decoders         *codec.Registry
	output           audio.Output
	active           audio.Playback
	runDone          chan struct{}
	closed           bool
}

// NewPlayer creates a player using the supplied stream, decoder, and audio output implementations.
func NewPlayer(streams StreamOpener, decoders *codec.Registry, output audio.Output) *Engine {
	return &Engine{
		volume: .75, reconnectEnabled: true, events: make(chan Event, 32), streams: streams,
		decoders: decoders, output: output,
	}
}

// Events returns the channel that receives playback state and metadata events.
func (p *Engine) Events() <-chan Event { return p.events }

// SetReconnect enables or disables automatic reconnect attempts.
func (p *Engine) SetReconnect(enabled bool) {
	p.mu.Lock()
	p.reconnectEnabled = enabled
	p.mu.Unlock()
}

// State returns the current playback state.
func (p *Engine) State() PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// emit queues an event without blocking the playback goroutine.
func (p *Engine) emit(event Event) {
	select {
	case p.events <- event:
	default:
	}
}

// setState updates playback state only while the supplied session remains active.
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

// SetVolume clamps volume to 0..1 and applies it to the active playback.
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

// Stop cancels playback and waits for the active session to finish closing.
func (p *Engine) Stop() {
	p.lifecycle.Lock()
	defer p.lifecycle.Unlock()
	p.stopSession()
}

// stopSession cancels the current session, closes its playback, and waits for its goroutine.
func (p *Engine) stopSession() {
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
	}
	p.session++
	p.cancel = nil
	playback := p.active
	runDone := p.runDone
	p.runDone = nil
	p.active = nil
	p.state = StateStopped
	p.emit(Event{State: StateStopped})
	p.mu.Unlock()
	if playback != nil {
		if err := playback.Close(); err != nil {
			slog.Warn("could not stop audio playback", "error", err)
		}
	}
	if runDone != nil {
		<-runDone
	}
}

// Play stops the current session and starts playing station asynchronously.
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
	p.runDone = make(chan struct{})
	runDone := p.runDone
	session := p.session
	p.mu.Unlock()
	go func() {
		defer close(runDone)
		p.run(ctx, session, station)
	}()
}

// run opens a stream, then retries failures while reconnect is enabled.
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
			err = ErrStreamEnded
		}
		slog.Error("radio playback attempt failed", "station", station.Name, "station_id", station.ID, "attempt", attempt+1, "error", err)
		if !retrypolicy.ShouldRetry(err) {
			p.setState(ctx, session, StateError, station, err, attempt)
			return
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
		timer := time.NewTimer(retryDelay(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// connect resolves, decodes, and plays one stream attempt until it ends or is canceled.
func (p *Engine) connect(ctx context.Context, session uint64, station Station) error {
	streamURL := station.URL
	if station.ResolvedURL != "" {
		streamURL = station.ResolvedURL
	}
	var info codec.StreamInfo
	stream, info, err := p.streams.Open(ctx, streamURL, func(raw string) {
		metadata := icy.ParseTrackMetadata(raw)
		if metadata.RawTitle == "" {
			return
		}
		p.mu.Lock()
		if p.session != session || ctx.Err() != nil {
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
	defer func() {
		if err := stream.Close(); err != nil {
			slog.Warn("could not close radio stream", "station", station.Name, "station_id", station.ID, "error", err)
		}
	}()
	if !p.setState(ctx, session, StateBuffering, station, nil, 0) {
		return ctx.Err()
	}
	decoded, err := p.decoders.Decode(stream, info)
	if err != nil {
		return fmt.Errorf("unable to decode %s stream: %w", info.Codec, err)
	}
	defer func() {
		if err := decoded.Close(); err != nil {
			slog.Warn("could not close audio decoder", "station", station.Name, "station_id", station.ID, "codec", info.Codec, "error", err)
		}
	}()
	if decoded.SampleRate() <= 0 || decoded.Channels() <= 0 {
		return retrypolicy.NoRetry(ErrInvalidPCMFormat)
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
		if err := playback.Close(); err != nil {
			slog.Warn("could not close audio playback", "station", station.Name, "station_id", station.ID, "error", err)
		}
	}()
	p.mu.Lock()
	if p.session != session || ctx.Err() != nil {
		p.mu.Unlock()
		return ctx.Err()
	}
	p.active = playback
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
				return fmt.Errorf("%w: %w", ErrAudioPlaybackFailed, err)
			}
			if !playback.IsPlaying() {
				return io.EOF
			}
		}
	}
}

// clearPlayback removes playback from the engine if it is still the active instance.
func (p *Engine) clearPlayback(playback audio.Playback) {
	p.mu.Lock()
	if p.active == playback {
		p.active = nil
	}
	p.mu.Unlock()
}

// Close stops playback and closes idle connections owned by the stream opener.
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
