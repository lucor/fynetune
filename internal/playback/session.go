// Package playback owns a radio player independently from any user interface.
package playback

import (
	"sync"

	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
)

// Event is a playback snapshot with optional, immutable station artwork.
// The audio engine remains independent of artwork loading and presentation.
type Event struct {
	radio.Event
	Artwork *favicon.Icon
}

// Session owns the player and is its only event consumer. Subscribers each get
// their own bounded channel containing the latest playback event.
type Session struct {
	player radio.Player

	mu             sync.Mutex
	current        Event
	nextID         uint64
	observers      map[uint64]chan Event
	closed         bool
	artworkStation radio.Station
	artwork        *favicon.Icon

	stop       chan struct{}
	eventsDone chan struct{}
	closeOnce  sync.Once
	closeErr   error
}

// NewSession starts owning the supplied player and forwards its events to
// independent observers.
func NewSession(player radio.Player) *Session {
	s := &Session{
		player:     player,
		current:    Event{Event: radio.Event{State: player.State()}},
		observers:  make(map[uint64]chan Event),
		stop:       make(chan struct{}),
		eventsDone: make(chan struct{}),
	}
	go s.forwardEvents()
	return s
}

// Play starts the selected station.
func (s *Session) Play(station radio.Station) {
	s.mu.Lock()
	if !sameArtworkSource(s.artworkStation, station) {
		s.artwork = nil
	}
	s.artworkStation = station
	s.mu.Unlock()
	s.player.Play(station)
}

// SetArtwork shares the image already loaded for the player. Late results from
// another station or icon source are ignored. Callers must not mutate the image.
func (s *Session) SetArtwork(station radio.Station, icon favicon.Icon) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	// The saved station's icon can load before the first Play command.
	if s.artworkStation.ID == "" {
		s.artworkStation = station
	}
	if !sameArtworkSource(s.artworkStation, station) {
		return
	}
	s.artwork = &icon
	if sameArtworkSource(s.current.Station, station) {
		s.current.Artwork = s.artwork
		for _, events := range s.observers {
			publishLatest(events, s.current)
		}
	}
}

func sameArtworkSource(a, b radio.Station) bool {
	return a.ID == b.ID && a.Favicon == b.Favicon && a.Homepage == b.Homepage
}

// Stop stops the current station.
func (s *Session) Stop() { s.player.Stop() }

// SetVolume updates the playback volume.
func (s *Session) SetVolume(volume float64) { s.player.SetVolume(volume) }

// SetReconnect enables or disables automatic reconnect attempts.
func (s *Session) SetReconnect(enabled bool) { s.player.SetReconnect(enabled) }

// State returns the player's current playback state.
func (s *Session) State() radio.PlayerState { return s.player.State() }

// Subscribe returns a private event stream initialized with the latest event.
// Its one-element buffer coalesces stale updates so slow observers stay bounded.
func (s *Session) Subscribe() (<-chan Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	events := make(chan Event, 1)
	events <- s.current
	if s.closed {
		close(events)
		return events, func() {}
	}
	s.observers[id] = events

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if channel, ok := s.observers[id]; ok {
				delete(s.observers, id)
				close(channel)
			}
		})
	}
	return events, unsubscribe
}

// Close stops the player and event forwarding. Repeated calls are safe.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.stop)
		s.closeErr = s.player.Close()
		<-s.eventsDone
	})
	return s.closeErr
}

func (s *Session) forwardEvents() {
	defer func() {
		s.mu.Lock()
		s.closed = true
		for id, events := range s.observers {
			delete(s.observers, id)
			close(events)
		}
		s.mu.Unlock()
		close(s.eventsDone)
	}()
	for {
		select {
		case <-s.stop:
			return
		case event, ok := <-s.player.Events():
			if !ok {
				return
			}
			s.mu.Lock()
			s.current = Event{Event: event}
			if sameArtworkSource(s.artworkStation, event.Station) {
				s.current.Artwork = s.artwork
			}
			for _, events := range s.observers {
				publishLatest(events, s.current)
			}
			s.mu.Unlock()
		}
	}
}

func publishLatest(events chan Event, event Event) {
	select {
	case events <- event:
		return
	default:
	}
	select {
	case <-events:
	default:
	}
	select {
	case events <- event:
	default:
	}
}
