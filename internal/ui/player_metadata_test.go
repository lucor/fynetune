package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"go.lucor.dev/fynetune/internal/metadata"
	"go.lucor.dev/fynetune/internal/radio"
)

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
