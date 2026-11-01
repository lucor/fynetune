package playback

import (
	"image"
	"sync"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/metadata"
	"go.lucor.dev/fynetune/internal/radio"
)

func TestSessionSharesArtworkWithoutLosingPlaybackMetadata(t *testing.T) {
	player := newTestPlayer(radio.StateStopped)
	session := NewSession(player)
	defer session.Close()
	events, unsubscribe := session.Subscribe()
	defer unsubscribe()
	receiveEvent(t, events)
	station := radio.Station{ID: "first", Favicon: "first.png"}
	// The saved station's artwork may load while the player is still idle.
	icon := favicon.Icon{Raster: image.NewNRGBA(image.Rect(0, 0, 16, 16))}
	session.SetArtwork(station, icon)
	session.Play(station)
	player.events <- radio.Event{State: radio.StatePlaying, Station: station, Metadata: metadata.Track{Title: "Song"}}
	first := receiveEvent(t, events)
	if first.Artwork == nil || first.Artwork.Raster != icon.Raster || first.Metadata.Title != "Song" {
		t.Fatalf("incomplete playback snapshot: %+v", first)
	}
	// Track updates retain the same immutable artwork.
	player.events <- radio.Event{State: radio.StatePlaying, Station: station, Metadata: metadata.Track{Title: "Next"}}
	next := receiveEvent(t, events)
	if next.Artwork != first.Artwork || next.Metadata.Title != "Next" {
		t.Fatal("track update lost artwork")
	}
	late, cancelLate := session.Subscribe()
	defer cancelLate()
	if got := receiveEvent(t, late); got.Artwork != first.Artwork || got.Metadata.Title != "Next" {
		t.Fatal("new observer did not receive current artwork and track")
	}
	// A changed icon source for the same station invalidates pending results.
	updated := station
	updated.Favicon = "new.png"
	session.Play(updated)
	session.SetArtwork(station, icon)
	player.events <- radio.Event{State: radio.StatePlaying, Station: updated}
	if got := receiveEvent(t, events); got.Artwork != nil {
		t.Fatal("old icon source survived station change")
	}
	session.SetArtwork(updated, icon)
	if got := receiveEvent(t, events); got.Artwork == nil {
		t.Fatal("current artwork was not published")
	}
	other := radio.Station{ID: "other"}
	session.Play(other)
	session.SetArtwork(updated, icon)
	player.events <- radio.Event{State: radio.StatePlaying, Station: other}
	if got := receiveEvent(t, events); got.Artwork != nil {
		t.Fatal("late artwork leaked into another station")
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	session.SetArtwork(other, icon) // Closed observers must never receive another send.
}

func TestSessionSubscriptionsReceiveIndependentEvents(t *testing.T) {
	player := newTestPlayer(radio.StateConnecting)
	session := NewSession(player)
	defer session.Close()

	first, unsubscribeFirst := session.Subscribe()
	defer unsubscribeFirst()
	second, unsubscribeSecond := session.Subscribe()
	defer unsubscribeSecond()

	if event := receiveEvent(t, first); event.State != radio.StateConnecting {
		t.Fatalf("first initial state = %v, want connecting", event.State)
	}
	if event := receiveEvent(t, second); event.State != radio.StateConnecting {
		t.Fatalf("second initial state = %v, want connecting", event.State)
	}

	playing := radio.Event{State: radio.StatePlaying, Station: radio.Station{ID: "station"}}
	player.events <- playing
	if event := receiveEvent(t, first); event.State != radio.StatePlaying || event.Station.ID != "station" {
		t.Fatalf("first update = %+v, want playing event", event)
	}
	if event := receiveEvent(t, second); event.State != radio.StatePlaying || event.Station.ID != "station" {
		t.Fatalf("second update = %+v, want playing event", event)
	}

	unsubscribeFirst()
	if _, ok := <-first; ok {
		t.Fatal("unsubscribed event stream remains open")
	}
	player.events <- radio.Event{State: radio.StateBuffering}
	if event := receiveEvent(t, second); event.State != radio.StateBuffering {
		t.Fatalf("remaining subscriber event = %+v, want buffering", event)
	}
}

func TestSessionSubscriptionGetsLatestStateAndCoalescesBacklog(t *testing.T) {
	player := newTestPlayer(radio.StateStopped)
	session := NewSession(player)
	defer session.Close()

	events, unsubscribe := session.Subscribe()
	defer unsubscribe()
	if event := receiveEvent(t, events); event.State != radio.StateStopped {
		t.Fatalf("initial state = %v, want stopped", event.State)
	}

	session.mu.Lock()
	player.events <- radio.Event{State: radio.StateConnecting}
	player.events <- radio.Event{State: radio.StatePlaying, Station: radio.Station{ID: "current"}}
	deadline := time.Now().Add(time.Second)
	for len(player.events) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(player.events) != 1 {
		session.mu.Unlock()
		t.Fatal("session did not receive the first event while its state lock was held")
	}
	session.mu.Unlock()
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		current := session.current
		session.mu.Unlock()
		if current.State == radio.StatePlaying {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if event := receiveEvent(t, events); event.State != radio.StatePlaying || event.Station.ID != "current" {
		t.Fatalf("latest event = %+v, want the newest playing state", event)
	}

	second, cancelSecond := session.Subscribe()
	defer cancelSecond()
	if event := receiveEvent(t, second); event.State != radio.StatePlaying || event.Station.ID != "current" {
		t.Fatalf("new subscriber snapshot = %+v, want current playing state", event)
	}
}

func TestSessionCloseIsIdempotentAndClosesSubscribers(t *testing.T) {
	player := newTestPlayer(radio.StateStopped)
	session := NewSession(player)
	events, unsubscribe := session.Subscribe()
	defer unsubscribe()
	_ = receiveEvent(t, events)

	if err := session.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := player.closeCalls(); got != 1 {
		t.Fatalf("player Close() calls = %d, want 1", got)
	}
	if _, ok := <-events; ok {
		t.Fatal("session close left subscriber channel open")
	}
}

func receiveEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("event stream closed before receiving an event")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for playback event")
		return Event{}
	}
}

type testPlayer struct {
	mu         sync.Mutex
	events     chan radio.Event
	state      radio.PlayerState
	closed     bool
	closeCount int
}

func newTestPlayer(state radio.PlayerState) *testPlayer {
	return &testPlayer{events: make(chan radio.Event, 8), state: state}
}

func (p *testPlayer) Play(radio.Station)         {}
func (p *testPlayer) Stop()                      {}
func (p *testPlayer) SetVolume(float64)          {}
func (p *testPlayer) SetReconnect(bool)          {}
func (p *testPlayer) Events() <-chan radio.Event { return p.events }

func (p *testPlayer) State() radio.PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *testPlayer) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCount++
	if !p.closed {
		p.closed = true
		close(p.events)
	}
	return nil
}

func (p *testPlayer) closeCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeCount
}
