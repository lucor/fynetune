// Package android keeps the Android foreground service aligned with
// the state of the Go playback session.
package android

import (
	"log/slog"
	"strings"
	"sync/atomic"

	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/playback"
	"go.lucor.dev/fynetune/internal/radio"
)

type serviceBridge interface {
	Update(mediaUpdate) error
	Stop() error
}

// mediaUpdate contains the values published to Android's media session.
type mediaUpdate struct {
	artwork     *favicon.Icon
	state       int
	station     string
	album       string
	description string
}

// playbackInfo maps a complete player event to android.media.session.PlaybackState
// values and media metadata. State-only events clear the previous track.
func playbackInfo(event radio.Event) mediaUpdate {
	update := mediaUpdate{
		station:     strings.TrimSpace(event.Station.Name),
		album:       strings.TrimSpace(event.Metadata.Album),
		description: playback.Description(event),
	}
	if update.station == "" {
		update.station = "FyneTune"
	}
	switch event.State {
	case radio.StateConnecting:
		update.state = 8 // PlaybackState.STATE_CONNECTING
	case radio.StateBuffering, radio.StateReconnecting:
		update.state = 6 // PlaybackState.STATE_BUFFERING
	case radio.StatePlaying:
		update.state = 3 // PlaybackState.STATE_PLAYING
	case radio.StateError:
		update.state = 7 // PlaybackState.STATE_ERROR
	default:
		update.state = 1 // PlaybackState.STATE_STOPPED
	}
	return update
}

var activeSession atomic.Pointer[playback.Session]

// StopPlaybackFromService handles the Stop action from the Android notification.
func StopPlaybackFromService() {
	if session := activeSession.Load(); session != nil {
		session.Stop()
	}
}

func attach(session *playback.Session, bridge serviceBridge) func() {
	events, unsubscribe := session.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer unsubscribe()
		active := false
		defer func() {
			if active {
				if err := bridge.Stop(); err != nil {
					slog.Warn("could not stop Android playback service", "error", err)
				}
			}
		}()

		for event := range events {
			shouldRun := keepServiceActive(event.State)
			if !shouldRun && !active {
				continue
			}
			var err error
			if shouldRun {
				update := playbackInfo(event.Event)
				update.artwork = event.Artwork
				err = bridge.Update(update)
			} else {
				err = bridge.Stop()
			}
			if err != nil {
				slog.Warn("could not update Android playback service", "state", event.State, "error", err)
				continue
			}
			active = shouldRun
		}
	}()

	var closed atomic.Bool
	return func() {
		if closed.CompareAndSwap(false, true) {
			unsubscribe()
			<-done
		}
	}
}

// keepServiceActive documents the states that require the foreground service.
func keepServiceActive(state radio.PlayerState) bool {
	return state != radio.StateStopped && state != radio.StateError
}
