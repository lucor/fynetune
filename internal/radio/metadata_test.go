package radio

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/metadata"
)

func TestMetadataReceivedDuringOpenIsPublishedWithPlayback(t *testing.T) {
	track := metadata.Track{Artist: "Artist", Title: "Song", Album: "Album", Year: "2000", Parsed: true}
	streams := streamOpenerFunc(func(_ context.Context, url string, onMetadata func(metadata.Event)) (io.ReadCloser, codec.StreamInfo, error) {
		onMetadata(metadata.Event{Kind: metadata.Replace, Source: metadata.SourceICY, Track: track})
		return io.NopCloser(strings.NewReader("compressed")), codec.StreamInfo{URL: url, Codec: codec.NameMP3}, nil
	})
	player := NewPlayer(streams, testDecoders(t), testAudioOutput{})
	t.Cleanup(func() { _ = player.Close() })
	station := Station{ID: "radio", Name: "Radio", URL: "https://example.invalid/live"}
	player.Play(station)
	waitState(t, player, StatePlaying)
	events := playingEvents(player)
	if len(events) != 1 || events[0].Metadata != track || events[0].Station.ID != station.ID || events[0].StreamInfo.URL != station.URL || events[0].StreamInfo.Codec != codec.NameMP3 {
		t.Fatalf("initial playing events = %+v", events)
	}
}

func TestMetadataSnapshotsDuplicatesAndExplicitClear(t *testing.T) {
	callbacks := make(chan func(metadata.Event), 1)
	streams := streamOpenerFunc(func(_ context.Context, _ string, onMetadata func(metadata.Event)) (io.ReadCloser, codec.StreamInfo, error) {
		callbacks <- onMetadata
		return io.NopCloser(strings.NewReader("compressed")), codec.StreamInfo{Codec: codec.NameMP3}, nil
	})
	player := NewPlayer(streams, testDecoders(t), testAudioOutput{})
	t.Cleanup(func() { _ = player.Close() })
	player.Play(Station{ID: "radio", URL: "https://example.invalid/live"})
	callback := receiveMetadataCallback(t, callbacks)
	waitState(t, player, StatePlaying)
	playingEvents(player)
	first := metadata.Track{RawTitle: "Original", Artist: "Artist", Title: "Song", Album: "Album", Year: "2000", Parsed: true}
	callback(metadata.Event{Kind: metadata.Replace, Track: first})
	duplicate := first
	duplicate.RawTitle = "Different representation of the same fields"
	callback(metadata.Event{Kind: metadata.Replace, Track: duplicate})
	callback(metadata.Event{})
	callback(metadata.Event{Kind: metadata.Replace})
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != first {
		t.Fatalf("events after duplicate and absent metadata = %+v", events)
	}

	// A partial snapshot for the next track must not inherit the previous album.
	next := metadata.Track{Title: "Next song"}
	callback(metadata.Event{Kind: metadata.Replace, Track: next})
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != next {
		t.Fatalf("track change events = %+v", events)
	}
	callback(metadata.Event{Kind: metadata.Clear})
	callback(metadata.Event{Kind: metadata.Clear})
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != (metadata.Track{}) {
		t.Fatalf("clear events = %+v", events)
	}
	callback(metadata.Event{Kind: metadata.Replace, Track: next})
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != next {
		t.Fatalf("restored track events = %+v", events)
	}
}

func TestMetadataFromPreviousAttemptsAndStationsIsIgnored(t *testing.T) {
	callbacks := make(chan func(metadata.Event), 3)
	first := metadata.Track{Title: "Initial song"}
	opens := 0
	streams := streamOpenerFunc(func(_ context.Context, _ string, onMetadata func(metadata.Event)) (io.ReadCloser, codec.StreamInfo, error) {
		opens++
		onMetadata(metadata.Event{Kind: metadata.Replace, Track: first})
		callbacks <- onMetadata
		if opens == 1 {
			return nil, codec.StreamInfo{}, io.ErrUnexpectedEOF
		}
		return io.NopCloser(strings.NewReader("compressed")), codec.StreamInfo{Codec: codec.NameMP3}, nil
	})
	player := NewPlayer(streams, testDecoders(t), testAudioOutput{})
	t.Cleanup(func() { _ = player.Close() })
	player.Play(Station{ID: "old", URL: "https://example.invalid/old"})
	failedCallback := receiveMetadataCallback(t, callbacks)
	oldCallback := receiveMetadataCallback(t, callbacks)
	waitState(t, player, StatePlaying)
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != first {
		t.Fatalf("reconnected playback events = %+v", events)
	}
	late := metadata.Event{Kind: metadata.Replace, Track: metadata.Track{Title: "Stale song"}}
	failedCallback(late)
	if events := playingEvents(player); len(events) != 0 {
		t.Fatalf("failed attempt emitted metadata after reconnect: %+v", events)
	}
	current := metadata.Event{Kind: metadata.Replace, Track: metadata.Track{Title: "Current song"}}
	oldCallback(current)
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != current.Track {
		t.Fatalf("current attempt metadata = %+v", events)
	}

	player.Play(Station{ID: "new", URL: "https://example.invalid/new"})
	newCallback := receiveMetadataCallback(t, callbacks)
	waitState(t, player, StatePlaying)
	if events := playingEvents(player); len(events) != 1 || events[0].Station.ID != "new" || events[0].Metadata != first {
		t.Fatalf("new station events = %+v", events)
	}
	oldCallback(late)
	if events := playingEvents(player); len(events) != 0 {
		t.Fatalf("previous station emitted metadata: %+v", events)
	}
	newCallback(current)
	if events := playingEvents(player); len(events) != 1 || events[0].Metadata != current.Track {
		t.Fatalf("new station metadata = %+v", events)
	}
	player.Stop()
	playingEvents(player)
	newCallback(late)
	if events := playingEvents(player); len(events) != 0 {
		t.Fatalf("stopped station emitted metadata: %+v", events)
	}
}

func receiveMetadataCallback(t *testing.T, callbacks <-chan func(metadata.Event)) func(metadata.Event) {
	t.Helper()
	select {
	case callback := <-callbacks:
		return callback
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for metadata callback")
		return nil
	}
}

func playingEvents(player *Engine) []Event {
	var events []Event
	for {
		select {
		case event, ok := <-player.Events():
			if !ok {
				return events
			}
			if event.State == StatePlaying {
				events = append(events, event)
			}
		default:
			return events
		}
	}
}
