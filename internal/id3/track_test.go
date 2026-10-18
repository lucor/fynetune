package id3

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"go.lucor.dev/fynetune/internal/metadata"
)

func TestTrackStandardFieldsAndGrouping(t *testing.T) {
	for _, tc := range []struct {
		name string
		tag  Tag
		want metadata.Track
	}{
		{
			name: "structured fields and release date",
			tag: Tag{TextFrames: map[string][]string{
				"TPE1": {" Artist ", "Guest"}, "TIT2": {"Song - Live"}, "TALB": {"Album"},
				"TDRL": {"2020-02"}, "TDRC": {"2019-01-01"}, "TYER": {"2018"},
				"TIT1": {"Other artist - Other song"},
			}},
			want: metadata.Track{Artist: "Artist, Guest", Title: "Song - Live", Album: "Album", Year: "2020", Parsed: true},
		},
		{
			name: "standard grouping",
			tag:  Tag{TextFrames: map[string][]string{"TIT1": {"AC-DC - Thunderstruck"}}},
			want: metadata.Track{RawTitle: "AC-DC - Thunderstruck", Artist: "AC-DC", Title: "Thunderstruck", Parsed: true},
		},
		{
			name: "user-defined grouping",
			tag:  Tag{UserTextFrames: map[string][]string{"GROUPING": {" Artist | Song \x00"}}},
			want: metadata.Track{RawTitle: "Artist | Song", Artist: "Artist", Title: "Song", Parsed: true},
		},
		{
			name: "standard grouping precedes user-defined grouping",
			tag: Tag{TextFrames: map[string][]string{"TIT1": {"Artist - Song"}},
				UserTextFrames: map[string][]string{"grouping": {"Another artist - Another song"}}},
			want: metadata.Track{RawTitle: "Artist - Song", Artist: "Artist", Title: "Song", Parsed: true},
		},
		{
			name: "grouping with matching standard artist",
			tag:  Tag{TextFrames: map[string][]string{"TPE1": {"Artist"}, "TIT1": {"Artist - Song"}}},
			want: metadata.Track{RawTitle: "Artist - Song", Artist: "Artist", Title: "Song", Parsed: true},
		},
		{
			name: "conflicting grouping preserves standard artist",
			tag:  Tag{TextFrames: map[string][]string{"TPE1": {"Artist"}, "TIT1": {"Another artist - Song"}}},
			want: metadata.Track{RawTitle: "Another artist - Song", Artist: "Artist", Title: "Another artist - Song", Parsed: true},
		},
		{
			name: "ambiguous grouping stays unstructured",
			tag:  Tag{TextFrames: map[string][]string{"TIT1": {"Artist ~ Song - Live"}}},
			want: metadata.Track{RawTitle: "Artist ~ Song - Live", Title: "Artist ~ Song - Live"},
		},
		{
			name: "invalid dates fall back to legacy year",
			tag:  Tag{TextFrames: map[string][]string{"TIT2": {"Song"}, "TDRL": {"2024-02-30"}, "TDRC": {"2024garbage"}, "TYER": {"2000"}}},
			want: metadata.Track{Title: "Song", Year: "2000"},
		},
		{
			name: "recording timestamp",
			tag:  Tag{TextFrames: map[string][]string{"TIT2": {"Song"}, "TDRC": {"2024-02-29T12:34:56Z"}}},
			want: metadata.Track{Title: "Song", Year: "2024"},
		},
		{
			name: "title delimiters are preserved",
			tag:  Tag{TextFrames: map[string][]string{"TIT2": {"Song - Live"}}},
			want: metadata.Track{Title: "Song - Live"},
		},
		{name: "no metadata"},
		{name: "lyricist is not a title", tag: Tag{TextFrames: map[string][]string{"TEXT": {"Writer"}}}},
		{name: "JSON is not interpreted", tag: Tag{TextFrames: map[string][]string{"TEXT": {`{"artist":"Artist","title":"Song"}`}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.tag)
			if got := tc.tag.Track(); got != tc.want {
				t.Fatalf("Track() = %+v, want %+v", got, tc.want)
			}
			after, _ := json.Marshal(tc.tag)
			if !bytes.Equal(before, after) {
				t.Fatal("Track modified source fields")
			}
		})
	}
}

func TestTrackFromLocalFixtures(t *testing.T) {
	for _, tc := range []struct {
		file string
		want metadata.Track
	}{
		{"standard-v23.id3", metadata.Track{Artist: "Beyoncé", Title: "Déjà Vu", Album: "B'Day", Year: "2006", Parsed: true}},
		{"standard-v24.id3", metadata.Track{Artist: "Artist, Guest", Title: "Song 🎵", Album: "Album", Year: "2026", Parsed: true}},
	} {
		tag, err := Parse(readFixture(t, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		if got := tag.Track(); got != tc.want {
			t.Fatalf("%s: Track() = %+v, want %+v", tc.file, got, tc.want)
		}
	}
}

func TestTrackDoesNotCarryFieldsBetweenSongs(t *testing.T) {
	first := Tag{TextFrames: map[string][]string{"TIT2": {"First song"}, "TALB": {"First album"}}}.Track()
	next := Tag{TextFrames: map[string][]string{"TIT2": {"Next song"}}}.Track()
	if first.Album != "First album" || next.Album != "" || next.Title != "Next song" {
		t.Fatalf("first = %+v, next = %+v", first, next)
	}
}

func TestTrackLogsOpaqueInputAndIgnoredFields(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	raw := `{"artist":"Artist","title":"Song"}`
	track := Tag{TextFrames: map[string][]string{"TEXT": {raw}}}.Track()
	if track != (metadata.Track{}) {
		t.Fatalf("opaque JSON produced a track: %+v", track)
	}
	var record struct {
		Source  string `json:"source"`
		Input   string `json:"input"`
		Result  string `json:"result"`
		Ignored string `json:"ignored_fields"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	var logged Tag
	if err := json.Unmarshal([]byte(record.Input), &logged); err != nil {
		t.Fatal(err)
	}
	if record.Source != "id3" || record.Result != "no musical fields" || !reflect.DeepEqual(logged.TextFrames["TEXT"], []string{raw}) || !strings.Contains(record.Ignored, "TEXT") {
		t.Fatalf("diagnostics = %s", output.String())
	}
}
