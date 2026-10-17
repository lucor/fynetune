package icy

import (
	"strings"
	"testing"
)

func TestParseTrackMetadataSupportedSeparators(t *testing.T) {
	separators := []string{"-", "–", "—", "~", "|"}
	for _, separator := range separators {
		for _, spacing := range []string{" ", ""} {
			t.Run(separator+"/"+spacing, func(t *testing.T) {
				got := ParseTrackMetadata("Artist" + spacing + separator + spacing + "Title")
				if !got.Parsed || got.Artist != "Artist" || got.Title != "Title" {
					t.Fatalf("ParseTrackMetadata() = %+v", got)
				}
			})
		}
	}
}

func TestParseTrackMetadataNormalizationAndFallback(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantRaw    string
		wantArtist string
		wantTitle  string
		wantParsed bool
	}{
		{name: "trim whitespace and trailing NUL", input: "  Artist | Title  \x00\x00", wantRaw: "Artist | Title", wantArtist: "Artist", wantTitle: "Title", wantParsed: true},
		{name: "trim spaces after NUL", input: "Artist - Title\x00  ", wantRaw: "Artist - Title", wantArtist: "Artist", wantTitle: "Title", wantParsed: true},
		{name: "apostrophes", input: "Guns N' Roses - Don't Cry", wantRaw: "Guns N' Roses - Don't Cry", wantArtist: "Guns N' Roses", wantTitle: "Don't Cry", wantParsed: true},
		{name: "hyphenated artist", input: "AC-DC - Thunderstruck", wantRaw: "AC-DC - Thunderstruck", wantArtist: "AC-DC", wantTitle: "Thunderstruck", wantParsed: true},
		{name: "empty artist", input: " - Track", wantRaw: "- Track", wantTitle: "- Track"},
		{name: "empty title", input: "Artist - ", wantRaw: "Artist -", wantTitle: "Artist -"},
		{name: "plain station name", input: "  Radio One  ", wantRaw: "Radio One", wantTitle: "Radio One"},
		{name: "slash is not a separator", input: "Artist / Title", wantRaw: "Artist / Title", wantTitle: "Artist / Title"},
		{name: "colon is not a separator", input: "Artist: Title", wantRaw: "Artist: Title", wantTitle: "Artist: Title"},
		{name: "empty metadata", input: " \x00\x00", wantRaw: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ParseTrackMetadata(test.input)
			if got.RawTitle != test.wantRaw || got.Artist != test.wantArtist || got.Title != test.wantTitle || got.Parsed != test.wantParsed {
				t.Fatalf("ParseTrackMetadata() = %+v, want raw=%q artist=%q title=%q parsed=%t", got, test.wantRaw, test.wantArtist, test.wantTitle, test.wantParsed)
			}
		})
	}
}

func TestParseTrackMetadataLeavesAmbiguousTitleUnsplit(t *testing.T) {
	got := ParseTrackMetadata("Artist ~ Song - Live")
	if got.Parsed || got.RawTitle != "Artist ~ Song - Live" || got.Title != got.RawTitle {
		t.Fatalf("ambiguous title = %+v", got)
	}
}

func TestParseTrackMetadataExtendedStationFields(t *testing.T) {
	tests := []struct {
		raw, artist, title, album, year string
	}{
		{
			raw:    "FONTAINES D.C.~MARIANNE~DOPAMINE CHAMBER~2026~~221~2026-09-25T10:30:43~2026-09-25T10:30:43~Virgin Radio",
			artist: "FONTAINES D.C.", title: "MARIANNE", album: "DOPAMINE CHAMBER", year: "2026",
		},
		{
			raw:    "R.E.M.~AFTERMATH~AROUND THE SUN~2004~~224~2026-09-25T10:35:22~2026-09-25T10:35:22~Virgin Radio",
			artist: "R.E.M.", title: "AFTERMATH", album: "AROUND THE SUN", year: "2004",
		},
	}
	for _, test := range tests {
		got := ParseTrackMetadata(test.raw)
		if !got.Parsed || got.RawTitle != test.raw || got.Artist != test.artist || got.Title != test.title || got.Album != test.album || got.Year != test.year {
			t.Errorf("ParseTrackMetadata(%q) = %+v", test.raw, got)
		}
		if got.String() != strings.Join([]string{test.artist, test.title, test.album, test.year}, " - ") {
			t.Errorf("String() = %q", got.String())
		}
	}
}
