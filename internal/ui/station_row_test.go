package ui

import (
	"testing"

	"go.lucor.dev/fynetune/internal/radio"
)

func TestStationMetaSeparatesGenreFromTechnicalMetadata(t *testing.T) {
	details := stationMeta(radio.Station{
		URL:     "https://radio.example/stream",
		Country: "Italy",
		Codec:   "mp3",
		Bitrate: 256,
		Tags:    []string{"news", "talk"},
	}, "Played 15:04")

	if details.genre != "news" {
		t.Errorf("genre = %q, want news", details.genre)
	}
	if details.metadata != "Italy · Played 15:04" {
		t.Errorf("metadata = %q", details.metadata)
	}
	if details.codec != "MP3 · 256 kbps" {
		t.Errorf("technical details = %q, want MP3 · 256 kbps", details.codec)
	}
}

func TestStationMetaOmitsUnavailableTechnicalDetails(t *testing.T) {
	for _, codec := range []string{"", "unknown", " Unknown "} {
		details := stationMeta(radio.Station{Codec: codec}, "")
		if details.codec != "" {
			t.Errorf("codec %q: technical details = %q, want empty", codec, details.codec)
		}
	}
}

func TestStationMetaFallsBackToHost(t *testing.T) {
	details := stationMeta(radio.Station{URL: "https://radio.example/live"}, "")
	if details.genre != "" {
		t.Errorf("genre = %q, want empty", details.genre)
	}
	if details.metadata != "radio.example" {
		t.Errorf("metadata = %q, want radio.example", details.metadata)
	}
}

func TestStationRowDetailViewKeepsMetadataReadable(t *testing.T) {
	view := stationRowDetailView(stationRowDetails{
		genre:    "news",
		metadata: "Italy",
		codec:    "MP3",
	})
	if got, want := view.Segments[0].Textual(), "news · Italy\nMP3"; got != want {
		t.Fatalf("detail text = %q, want %q", got, want)
	}
}
