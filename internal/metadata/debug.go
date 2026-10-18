package metadata

import (
	"context"
	"encoding/json"
	"log/slog"
	"unicode/utf8"
)

const (
	debugInputLimit = 4096
	debugFieldLimit = 1024
)

// DebugNormalization logs source input, the resulting track, and ignored fields
// at debug level. Input is a raw title or decoded source fields; ignored maps
// field names to reasons. Representations are bounded without modifying data
// used for playback. Serialization is skipped when debug logging is disabled.
func DebugNormalization(source Source, input any, track Track, ignored map[string]string) {
	logger := slog.Default()
	if !logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	text := debugInput(input)
	result := "normalized"
	if track.String() == "" {
		result = "no musical fields"
	}
	attrs := []any{
		"source", source,
		"input", debugText(text, debugInputLimit),
		"input_truncated", len(text) > debugInputLimit,
		"result", result,
		"track", slog.GroupValue(
			slog.String("raw_title", debugText(track.RawTitle, debugFieldLimit)),
			slog.String("artist", debugText(track.Artist, debugFieldLimit)),
			slog.String("title", debugText(track.Title, debugFieldLimit)),
			slog.String("album", debugText(track.Album, debugFieldLimit)),
			slog.String("year", debugText(track.Year, debugFieldLimit)),
			slog.Bool("parsed", track.Parsed),
		),
	}
	if len(ignored) > 0 {
		attrs = append(attrs, "ignored_fields", debugText(debugInput(ignored), debugInputLimit))
	}
	logger.Debug("stream metadata normalized", attrs...)
}

// debugInput preserves raw titles and serializes structured fields for logging.
func debugInput(input any) string {
	if text, ok := input.(string); ok {
		return text
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "unavailable: " + err.Error()
	}
	return string(data)
}

// debugText truncates at a UTF-8 boundary and marks the omitted suffix.
func debugText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end] + "…"
}
