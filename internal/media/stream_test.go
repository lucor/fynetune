package media

import (
	"strings"
	"testing"
)

func TestTrackMetadataParserSupportedSeparators(t *testing.T) {
	separators := []string{"-", "–", "—", "~", "|"}
	for _, separator := range separators {
		for _, spacing := range []string{" ", ""} {
			t.Run(separator+"/"+spacing, func(t *testing.T) {
				parser := NewTrackMetadataParser()
				got := parser.Parse("Artist" + spacing + separator + spacing + "Title")
				if !got.Parsed || got.Artist != "Artist" || got.Title != "Title" {
					t.Fatalf("Parse() = %+v", got)
				}
			})
		}
	}
}

func TestTrackMetadataParserNormalizationAndFallback(t *testing.T) {
	parser := NewTrackMetadataParser()
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
			got := parser.Parse(test.input)
			if got.RawTitle != test.wantRaw || got.Artist != test.wantArtist || got.Title != test.wantTitle || got.Parsed != test.wantParsed {
				t.Fatalf("Parse() = %+v, want raw=%q artist=%q title=%q parsed=%t", got, test.wantRaw, test.wantArtist, test.wantTitle, test.wantParsed)
			}
		})
	}
}

func TestRepeatedSeparatorInOneTitleIsOneObservation(t *testing.T) {
	parser := NewTrackMetadataParser()
	parser.Parse("Artist - Song - Live")
	if parser.totalSeen != 1 || len(parser.observed) != 1 {
		t.Fatalf("one title produced %d observations: %q", parser.totalSeen, string(parser.observed))
	}
}

func TestTrackMetadataParserLearnsSeparator(t *testing.T) {
	tests := []struct {
		name         string
		observations []string
		want         rune
	}{
		{name: "four consistent", observations: []string{"A ~ 1", "B ~ 2", "C ~ 3", "D ~ 4"}, want: '~'},
		{name: "three of four", observations: []string{"A ~ 1", "B - 2", "C ~ 3", "D ~ 4"}, want: '~'},
		{name: "two versus two", observations: []string{"A ~ 1", "B - 2", "C ~ 3", "D - 4"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parser := NewTrackMetadataParser()
			for _, observation := range test.observations {
				parser.Parse(observation)
			}
			if parser.preferred != test.want {
				t.Fatalf("preferred separator = %q, want %q", parser.preferred, test.want)
			}
		})
	}
}

func TestTrackMetadataParserContinuesLearningAfterTiedWindow(t *testing.T) {
	parser := NewTrackMetadataParser()
	for _, title := range []string{"A ~ 1", "B - 2", "C ~ 3", "D - 4"} {
		parser.Parse(title)
	}
	if parser.preferred != 0 {
		t.Fatalf("preferred separator after tie = %q, want unknown", parser.preferred)
	}
	parser.Parse("E ~ 5")
	if parser.preferred != '~' {
		t.Fatalf("preferred separator after new evidence = %q, want '~'", parser.preferred)
	}
}

func TestTrackMetadataParserDuplicateAndAmbiguousObservations(t *testing.T) {
	parser := NewTrackMetadataParser()
	for _, title := range []string{"A ~ 1", "A ~ 1", "B - 2", "C ~ 3", "D - 4"} {
		parser.Parse(title)
	}
	if parser.totalSeen != 4 || parser.preferred != 0 {
		t.Fatalf("duplicate affected learning: observations=%d preferred=%q", parser.totalSeen, parser.preferred)
	}
	parser.Parse("E ~ Song - Live")
	if parser.totalSeen != 4 || parser.preferred != 0 {
		t.Fatalf("ambiguous observation affected learning: observations=%d preferred=%q", parser.totalSeen, parser.preferred)
	}
}

func TestTrackMetadataParserUsesPreferredSeparatorAndPreservesSuffix(t *testing.T) {
	parser := NewTrackMetadataParser()
	for _, title := range []string{"One ~ First", "Two ~ Second", "Three ~ Third", "Four ~ Fourth"} {
		parser.Parse(title)
	}

	got := parser.Parse("Pink Floyd ~ Another Brick in the Wall - Part 2")
	if !got.Parsed || got.Artist != "Pink Floyd" || got.Title != "Another Brick in the Wall - Part 2" {
		t.Fatalf("Parse() = %+v", got)
	}
	parser.Parse("E ~ Song - Live") // competing separators are not learning evidence
	if parser.preferred != '~' {
		t.Fatalf("ambiguous update changed preferred separator to %q", parser.preferred)
	}

	got = parser.Parse("AC-DC - Thunderstruck")
	if got.Parsed || got.RawTitle != "AC-DC - Thunderstruck" || got.Title != got.RawTitle {
		t.Fatalf("preferred separator absence was parsed speculatively: %+v", got)
	}
}

func TestTrackMetadataParserDoesNotSplitAmbiguousTitleBeforeLearning(t *testing.T) {
	parser := NewTrackMetadataParser()
	got := parser.Parse("Artist ~ Song - Live")
	if got.Parsed || got.RawTitle != "Artist ~ Song - Live" || got.Title != got.RawTitle {
		t.Fatalf("ambiguous title = %+v", got)
	}
}

func TestTrackMetadataParserParsesExtendedStationFields(t *testing.T) {
	parser := NewTrackMetadataParser()
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
		got := parser.Parse(test.raw)
		if !got.Parsed || got.RawTitle != test.raw || got.Artist != test.artist || got.Title != test.title || got.Album != test.album || got.Year != test.year {
			t.Errorf("Parse(%q) = %+v", test.raw, got)
		}
		if got.String() != strings.Join([]string{test.artist, test.title, test.album, test.year}, " - ") {
			t.Errorf("String() = %q", got.String())
		}
	}
	if parser.totalSeen != 2 || string(parser.observed) != "~~" {
		t.Fatalf("date hyphens affected learning: observations=%q", string(parser.observed))
	}
}

func TestTrackMetadataStringOmitsEmptyFields(t *testing.T) {
	tests := []struct {
		metadata TrackMetadata
		want     string
	}{
		{metadata: TrackMetadata{Artist: "Artist", Title: "Song", Year: "2000"}, want: "Artist - Song - 2000"},
		{metadata: TrackMetadata{Artist: "Artist", Album: "Album"}, want: "Artist - Album"},
		{metadata: TrackMetadata{RawTitle: "Station ID"}, want: "Station ID"},
	}
	for _, test := range tests {
		if got := test.metadata.String(); got != test.want {
			t.Errorf("String() = %q, want %q", got, test.want)
		}
	}
}

func TestTrackMetadataParserLearnsSongSeparatorAfterStationIdentification(t *testing.T) {
	parser := NewTrackMetadataParser()
	parser.Parse("Virgin Radio - Style Rock")
	for _, raw := range []string{
		"FONTAINES D.C.~MARIANNE~DOPAMINE CHAMBER~2026~~221~2026-09-25T10:30:43~2026-09-25T10:30:43~Virgin Radio",
		"Artist Two~Song Two~Album~2026~~221~2026-09-25T10:33:43~2026-09-25T10:33:43~Virgin Radio",
		"Artist Three~Song Three~Album~2026~~221~2026-09-25T10:36:43~2026-09-25T10:36:43~Virgin Radio",
	} {
		parser.Parse(raw)
	}
	if parser.preferred != '~' {
		t.Fatalf("preferred separator = %q, want '~'", parser.preferred)
	}

	got := parser.Parse("Virgin Radio - Style Rock")
	if got.Parsed || got.Title != got.RawTitle {
		t.Fatalf("station identification parsed as track metadata after learning: %+v", got)
	}
}

func TestTrackMetadataParserChangesPreferredSeparatorAfterSustainedEvidence(t *testing.T) {
	parser := NewTrackMetadataParser()
	for _, title := range []string{"A ~ 1", "B ~ 2", "C ~ 3", "D ~ 4"} {
		parser.Parse(title)
	}
	for _, title := range []string{"E - 5", "F - 6", "G - 7", "H - 8"} {
		parser.Parse(title)
	}
	if parser.preferred != '~' {
		t.Fatalf("preferred separator changed on a tied window: %q", parser.preferred)
	}
	parser.Parse("I - 9")
	if parser.preferred != '-' {
		t.Fatalf("preferred separator = %q, want '-'; evidence=%q", parser.preferred, string(parser.observed))
	}
}

func TestTrackMetadataParserKeepsProvisionalParsingBeforeLearning(t *testing.T) {
	parser := NewTrackMetadataParser()
	got := parser.Parse("Artist~Title")
	if !got.Parsed || got.Artist != "Artist" || got.Title != "Title" || parser.preferred != 0 {
		t.Fatalf("provisional Parse() = %+v, preferred=%q", got, parser.preferred)
	}
}
