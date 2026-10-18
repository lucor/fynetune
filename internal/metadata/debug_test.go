package metadata

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDebugNormalizationBoundsDiagnostics(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	raw := strings.Repeat("🎵", debugInputLimit)
	track := Track{Artist: raw, Title: "Song"}
	DebugNormalization(SourceID3, raw, track, nil)
	var record struct {
		Input          string `json:"input"`
		InputTruncated bool   `json:"input_truncated"`
		Track          struct {
			Artist string `json:"artist"`
		} `json:"track"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if !record.InputTruncated || len(record.Input) > debugInputLimit+len("…") || len(record.Track.Artist) > debugFieldLimit+len("…") {
		t.Fatalf("unbounded diagnostics: input %d bytes, artist %d bytes", len(record.Input), len(record.Track.Artist))
	}
	if !utf8.ValidString(record.Input) || !utf8.ValidString(record.Track.Artist) || !strings.HasSuffix(record.Input, "…") {
		t.Fatal("diagnostic truncation must preserve UTF-8 and mark omitted text")
	}
	if track.Artist != raw {
		t.Fatal("diagnostics changed the track")
	}
}

func TestDebugNormalizationSkipsSerializationWhenDisabled(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	var serialized bool
	DebugNormalization(SourceID3, serializationProbe{called: &serialized}, Track{Title: "Song"}, nil)
	if serialized || output.Len() != 0 {
		t.Fatal("disabled debug logging must not serialize or emit metadata")
	}
}

type serializationProbe struct{ called *bool }

func (p serializationProbe) MarshalJSON() ([]byte, error) {
	*p.called = true
	return []byte(`{}`), nil
}
