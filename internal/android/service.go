// Package android keeps the Android foreground service aligned with
// the state of the Go playback session.
package android

import (
	"log/slog"
	"sync/atomic"

	"go.lucor.dev/fynetune/internal/playback"
	"go.lucor.dev/fynetune/internal/radio"
)

type serviceBridge interface {
	Start() error
	Stop() error
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
			if shouldRun == active {
				continue
			}
			var err error
			if shouldRun {
				err = bridge.Start()
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
