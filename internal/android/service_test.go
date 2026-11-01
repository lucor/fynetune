package android

import (
	"errors"
	"image"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/metadata"
	"go.lucor.dev/fynetune/internal/playback"
	"go.lucor.dev/fynetune/internal/radio"
)

func TestArtworkUpdateReachesServiceWithoutAnotherTrackEvent(t *testing.T) {
	player := &serviceTestPlayer{events: make(chan radio.Event, 8)}
	session := newServiceTestSession(player)
	bridge := &testServiceBridge{calls: make(chan serviceCall, 8)}
	detach := attach(session, bridge)
	t.Cleanup(func() { detach(); _ = session.Close() })
	station := radio.Station{ID: "radio", Name: "Radio"}
	session.Play(station)
	player.events <- radio.Event{State: radio.StatePlaying, Station: station, Metadata: metadata.Track{Artist: "Artist", Title: "Song"}}
	first := receiveCall(t, bridge.calls).update
	icon := favicon.Icon{Raster: image.NewNRGBA(image.Rect(0, 0, 16, 16))}
	session.SetArtwork(station, icon)
	updated := receiveCall(t, bridge.calls).update
	if updated.artwork == nil || updated.artwork.Raster != icon.Raster {
		t.Fatal("service did not receive player artwork")
	}
	updated.artwork = nil
	if updated != first {
		t.Fatal("artwork update changed playback metadata")
	}
}

func TestAttachTracksPlaybackAndStopsServiceOnDetach(t *testing.T) {
	player := &serviceTestPlayer{events: make(chan radio.Event, 8)}
	session := newServiceTestSession(player)
	bridge := &testServiceBridge{calls: make(chan serviceCall, 8)}
	detach := attach(session, bridge)
	t.Cleanup(func() {
		detach()
		_ = session.Close()
	})

	for _, event := range []radio.Event{
		{State: radio.StateConnecting, Station: radio.Station{Name: "Radio"}},
		{State: radio.StatePlaying, Station: radio.Station{Name: "Radio"}},
		{State: radio.StatePlaying, Station: radio.Station{Name: "Radio"}, Metadata: metadata.Track{Artist: "Artist", Title: "Song"}},
		{State: radio.StatePlaying, Station: radio.Station{Name: "Radio"}}, // Clear the previous track.
		{State: radio.StateReconnecting, Station: radio.Station{Name: "Radio"}},
	} {
		player.events <- event
		if call := receiveCall(t, bridge.calls); call.stopped || call.update != playbackInfo(event) {
			t.Fatalf("service update = %+v, want %+v", call, playbackInfo(event))
		}
	}
	player.events <- radio.Event{State: radio.StateStopped}
	if call := receiveCall(t, bridge.calls); !call.stopped {
		t.Fatalf("service call after terminal state = %+v, want stop", call)
	}
	player.events <- radio.Event{State: radio.StateBuffering}
	if call := receiveCall(t, bridge.calls); call.stopped || call.update.state != 6 {
		t.Fatalf("service call after playback resumed = %+v, want buffering", call)
	}

	detach()
	if call := receiveCall(t, bridge.calls); !call.stopped {
		t.Fatalf("service call on detach = %+v, want stop", call)
	}
	detach()
	select {
	case call := <-bridge.calls:
		t.Fatalf("unexpected extra service call %+v", call)
	default:
	}
}

func TestPlaybackInfo(t *testing.T) {
	for _, tc := range []struct {
		state       radio.PlayerState
		wantState   int
		description string
	}{
		{radio.StateConnecting, 8, "Connecting…"},
		{radio.StateBuffering, 6, "Buffering…"},
		{radio.StatePlaying, 3, "Now playing"},
		{radio.StateReconnecting, 6, "Connection interrupted · retrying"},
		{radio.StateStopped, 1, "Not playing"},
		{radio.StateError, 7, "Playback failed"},
	} {
		got := playbackInfo(radio.Event{State: tc.state, Station: radio.Station{Name: " Radio "}})
		want := mediaUpdate{state: tc.wantState, station: "Radio", description: tc.description}
		if got != want {
			t.Errorf("state %v: media update = %+v, want %+v", tc.state, got, want)
		}
	}
	event := radio.Event{
		State: radio.StatePlaying, Station: radio.Station{Name: "Radio 日本 🎵"},
		Metadata: metadata.Track{Artist: "Björk", Title: "Jóga", Album: "Homogenic"},
	}
	got := playbackInfo(event)
	want := mediaUpdate{state: 3, station: "Radio 日本 🎵", album: "Homogenic", description: "Björk - Jóga - Homogenic"}
	if got != want {
		t.Fatalf("structured media update = %+v, want %+v", got, want)
	}
	event.Station.Name = ""
	event.Metadata = metadata.Track{RawTitle: "Station announcement"}
	got = playbackInfo(event)
	want = mediaUpdate{state: 3, station: "FyneTune", description: "Station announcement"}
	if got != want {
		t.Fatalf("unstructured media update = %+v, want %+v", got, want)
	}
}

func TestStopPlaybackFromServiceUsesExistingSession(t *testing.T) {
	player := &serviceTestPlayer{events: make(chan radio.Event, 8)}
	session := newServiceTestSession(player)
	bridge := &testServiceBridge{calls: make(chan serviceCall, 8)}
	detach := attach(session, bridge)
	previous := activeSession.Swap(session)
	t.Cleanup(func() {
		activeSession.Store(previous)
		detach()
		_ = session.Close()
	})
	player.events <- radio.Event{State: radio.StatePlaying}
	receiveCall(t, bridge.calls)
	StopPlaybackFromService()
	if call := receiveCall(t, bridge.calls); !call.stopped || player.stopCalls.Load() != 1 {
		t.Fatal("native Stop must stop the existing player and service")
	}
	activeSession.Store(nil)
	StopPlaybackFromService()
	if player.stopCalls.Load() != 1 {
		t.Fatal("Stop without an attached session must be a no-op")
	}
}

func TestFailedMetadataUpdateKeepsServiceCleanupActive(t *testing.T) {
	player := &serviceTestPlayer{events: make(chan radio.Event, 8)}
	session := newServiceTestSession(player)
	bridge := &testServiceBridge{calls: make(chan serviceCall, 8)}
	detach := attach(session, bridge)
	t.Cleanup(func() {
		detach()
		_ = session.Close()
	})
	player.events <- radio.Event{State: radio.StatePlaying}
	receiveCall(t, bridge.calls)
	bridge.failNext.Store(true)
	player.events <- radio.Event{State: radio.StatePlaying, Metadata: metadata.Track{Title: "Song"}}
	receiveCall(t, bridge.calls)
	player.events <- radio.Event{State: radio.StateError}
	if call := receiveCall(t, bridge.calls); !call.stopped {
		t.Fatal("a failed metadata update must not prevent service cleanup on playback failure")
	}
}

func TestKeepServiceActive(t *testing.T) {
	for _, state := range []radio.PlayerState{
		radio.StateConnecting,
		radio.StateBuffering,
		radio.StatePlaying,
		radio.StateReconnecting,
	} {
		if !keepServiceActive(state) {
			t.Errorf("keepServiceActive(%v) = false, want true", state)
		}
	}
	for _, state := range []radio.PlayerState{radio.StateStopped, radio.StateError} {
		if keepServiceActive(state) {
			t.Errorf("keepServiceActive(%v) = true, want false", state)
		}
	}
}

func newServiceTestSession(player *serviceTestPlayer) *playback.Session {
	return playback.NewSession(player)
}

func receiveCall(t *testing.T, calls <-chan serviceCall) serviceCall {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for service call")
		return serviceCall{}
	}
}

type serviceCall struct {
	update  mediaUpdate
	stopped bool
}

type testServiceBridge struct {
	calls    chan serviceCall
	failNext atomic.Bool
}

func (b *testServiceBridge) Update(update mediaUpdate) error {
	failed := b.failNext.Swap(false)
	b.calls <- serviceCall{update: update}
	if failed {
		return errors.New("metadata update failed")
	}
	return nil
}
func (b *testServiceBridge) Stop() error {
	b.calls <- serviceCall{stopped: true}
	return nil
}

type serviceTestPlayer struct {
	mu        sync.Mutex
	events    chan radio.Event
	state     radio.PlayerState
	stopCalls atomic.Int32
}

func (p *serviceTestPlayer) Play(radio.Station) {}
func (p *serviceTestPlayer) Stop() {
	p.stopCalls.Add(1)
	p.events <- radio.Event{State: radio.StateStopped}
}
func (p *serviceTestPlayer) SetVolume(float64)          {}
func (p *serviceTestPlayer) SetReconnect(bool)          {}
func (p *serviceTestPlayer) Events() <-chan radio.Event { return p.events }
func (p *serviceTestPlayer) Close() error               { return nil }
func (p *serviceTestPlayer) State() radio.PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}
