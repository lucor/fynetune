package ui

import (
	"image"
	"testing"

	"fyne.io/fyne/v2/test"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/metadata"
	"go.lucor.dev/fynetune/internal/playback"
	"go.lucor.dev/fynetune/internal/radio"
)

func TestPlayerDisplaysSharedArtworkAndMetadata(t *testing.T) {
	app := test.NewTempApp(t)
	station := radio.Station{ID: "radio", Name: "Radio"}
	w := &Window{app: app, selectedStation: station}
	w.playerBar = newPlayerWidget(w, .75, nil, nil, nil)
	window := test.NewTempWindow(t, w.playerBar.Container)
	window.Show()
	icon := &favicon.Icon{Raster: image.NewNRGBA(image.Rect(0, 0, 32, 32))}
	event := playback.Event{Event: radio.Event{State: radio.StatePlaying, Station: station, Metadata: metadata.Track{Artist: "Artist", Title: "Song"}}, Artwork: icon}
	w.updatePlayback(event)
	if w.playerBar.artwork.Image != icon.Raster || w.playerBar.title.Text != station.Name || w.playerBar.track.Text != playback.Description(event.Event) {
		t.Fatal("player does not display the shared artwork and metadata")
	}
	event.Station = radio.Station{ID: "stale", Name: "Other"}
	event.Artwork = &favicon.Icon{Raster: image.NewNRGBA(image.Rect(0, 0, 1, 1))}
	w.updatePlayback(event)
	if w.playerBar.artwork.Image != icon.Raster || w.playerBar.title.Text != station.Name {
		t.Fatal("stale snapshot changed player")
	}
	event.Station = station
	event.Artwork = nil
	event.Metadata = metadata.Track{}
	w.updatePlayback(event)
	if w.playerBar.artwork.Resource == nil || w.playerBar.track.Text != "Now playing" {
		t.Fatalf("fallback artwork = %v, track = %q", w.playerBar.artwork.Resource, w.playerBar.track.Text)
	}
}

func TestPlayerDisplaysStructuredMetadataAndClear(t *testing.T) {
	app := test.NewTempApp(t)
	station := radio.Station{ID: "radio", Name: "Radio"}
	w := &Window{app: app, selectedStation: station}
	w.playerBar = newPlayerWidget(w, .75, nil, nil, nil)
	window := test.NewTempWindow(t, w.playerBar.Container)
	window.Show()

	// The first playing event can already contain a structured track snapshot.
	w.updatePlayer(radio.Event{State: radio.StatePlaying, Station: station,
		Metadata: metadata.Track{Artist: "Artist", Title: "Song", Album: "Album", Year: "2000"}})
	if got, want := w.playerBar.track.Text, "Artist - Song - Album - 2000"; got != want {
		t.Fatalf("track text = %q, want %q", got, want)
	}
	if got := w.playerBar.title.Text; got != station.Name {
		t.Fatalf("station text = %q, want %q", got, station.Name)
	}
	w.updatePlayer(radio.Event{State: radio.StatePlaying, Station: station})
	if got := w.playerBar.track.Text; got != "Now playing" {
		t.Fatalf("cleared track text = %q", got)
	}
}
