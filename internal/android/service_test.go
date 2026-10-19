package android

import (
	"sync"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/playback"
	"go.lucor.dev/fynetune/internal/radio"
)

func TestAttachTracksPlaybackAndStopsServiceOnDetach(t *testing.T) {
	player := &serviceTestPlayer{events: make(chan radio.Event, 8)}
	session := newServiceTestSession(player)
	bridge := &testServiceBridge{calls: make(chan string, 8)}
	detach := attach(session, bridge)
	t.Cleanup(func() {
		detach()
		_ = session.Close()
	})

	player.events <- radio.Event{State: radio.StateConnecting}
	if call := receiveCall(t, bridge.calls); call != "start" {
		t.Fatalf("first service call = %q, want start", call)
	}
	player.events <- radio.Event{State: radio.StatePlaying}
	player.events <- radio.Event{State: radio.StateReconnecting}
	player.events <- radio.Event{State: radio.StateStopped}
	if call := receiveCall(t, bridge.calls); call != "stop" {
		t.Fatalf("service call after terminal state = %q, want stop", call)
	}
	player.events <- radio.Event{State: radio.StateBuffering}
	if call := receiveCall(t, bridge.calls); call != "start" {
		t.Fatalf("service call after playback resumed = %q, want start", call)
	}

	detach()
	if call := receiveCall(t, bridge.calls); call != "stop" {
		t.Fatalf("service call on detach = %q, want stop", call)
	}
	detach()
	select {
	case call := <-bridge.calls:
		t.Fatalf("unexpected extra service call %q", call)
	case <-time.After(20 * time.Millisecond):
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

func receiveCall(t *testing.T, calls <-chan string) string {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for service call")
		return ""
	}
}

type testServiceBridge struct{ calls chan string }

func (b *testServiceBridge) Start() error { b.calls <- "start"; return nil }
func (b *testServiceBridge) Stop() error  { b.calls <- "stop"; return nil }

type serviceTestPlayer struct {
	mu     sync.Mutex
	events chan radio.Event
	state  radio.PlayerState
}

func (p *serviceTestPlayer) Play(radio.Station)         {}
func (p *serviceTestPlayer) Stop()                      {}
func (p *serviceTestPlayer) SetVolume(float64)          {}
func (p *serviceTestPlayer) SetReconnect(bool)          {}
func (p *serviceTestPlayer) Events() <-chan radio.Event { return p.events }
func (p *serviceTestPlayer) Close() error               { return nil }
func (p *serviceTestPlayer) State() radio.PlayerState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}
