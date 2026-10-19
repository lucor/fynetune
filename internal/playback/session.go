// Package playback owns a radio player independently from any user interface.
package playback

import (
	"sync"

	"go.lucor.dev/fynetune/internal/radio"
)

// Session owns the player and is its only event consumer. Subscribers each get
// their own bounded channel containing the latest playback event.
type Session struct {
	player radio.Player

	mu        sync.Mutex
	current   radio.Event
	nextID    uint64
	observers map[uint64]chan radio.Event
	closed    bool

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
		current:    radio.Event{State: player.State()},
		observers:  make(map[uint64]chan radio.Event),
		stop:       make(chan struct{}),
		eventsDone: make(chan struct{}),
	}
	go s.forwardEvents()
	return s
}

// Play starts the selected station.
func (s *Session) Play(station radio.Station) { s.player.Play(station) }

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
func (s *Session) Subscribe() (<-chan radio.Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	id := s.nextID
	events := make(chan radio.Event, 1)
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
			s.current = event
			for _, events := range s.observers {
				publishLatest(events, event)
			}
			s.mu.Unlock()
		}
	}
}

func publishLatest(events chan radio.Event, event radio.Event) {
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
