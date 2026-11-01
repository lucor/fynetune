package playback

import "go.lucor.dev/fynetune/internal/radio"

// Description is the text shown by both the player and native media controls.
func Description(event radio.Event) string {
	switch event.State {
	case radio.StateConnecting:
		return "Connecting…"
	case radio.StateBuffering:
		return "Buffering…"
	case radio.StatePlaying:
		if track := event.Metadata.String(); track != "" {
			return track
		}
		return "Now playing"
	case radio.StateReconnecting:
		return "Connection interrupted · retrying"
	case radio.StateError:
		if event.Err != nil {
			return event.Err.Error()
		}
		return "Playback failed"
	default:
		return "Not playing"
	}
}
