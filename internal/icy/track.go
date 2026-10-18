package icy

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/metadata"
)

var uuidPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

// ParseTrackMetadata extracts track fields from an ICY StreamTitle and logs
// the original input and normalized track when debug logging is enabled.
// Extended ICY formats are interpreted before falling back to generic title text.
func ParseTrackMetadata(raw string) (track metadata.Track) {
	defer func() { metadata.DebugNormalization(metadata.SourceICY, raw, track, nil) }()
	track = metadata.ParseTitle(raw)
	if parsed, ok := parseExtendedTildeMetadata(track.RawTitle); ok {
		return parsed
	}
	if parsed, ok := parseSongStarMetadata(track.RawTitle); ok {
		return parsed
	}
	return track
}

// parseSongStarMetadata parses the Song*title*artist*year*UUID format.
func parseSongStarMetadata(raw string) (metadata.Track, bool) {
	fields := strings.Split(raw, "*")
	if len(fields) != 5 || !strings.EqualFold(strings.TrimSpace(fields[0]), "Song") {
		return metadata.Track{}, false
	}
	year := strings.TrimSpace(fields[3])
	if len(year) != 4 {
		return metadata.Track{}, false
	}
	if _, err := strconv.Atoi(year); err != nil {
		return metadata.Track{}, false
	}
	title := strings.TrimSpace(fields[1])
	artist := strings.TrimSpace(fields[2])
	uuid := strings.TrimSpace(fields[4])
	if title == "" || artist == "" || uuidPattern.FindString(uuid) != uuid {
		return metadata.Track{}, false
	}
	return metadata.Track{
		RawTitle: raw,
		Artist:   artist,
		Title:    title,
		Year:     year,
		Parsed:   true,
	}, true
}

// parseExtendedTildeMetadata parses the extended ICY fields used by some
// broadcasters: artist, title, album, year, duration, two timestamps, and station.
func parseExtendedTildeMetadata(raw string) (metadata.Track, bool) {
	fields := strings.Split(raw, "~")
	if len(fields) < 9 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
		return metadata.Track{}, false
	}
	if len(fields[3]) != 4 {
		return metadata.Track{}, false
	}
	if _, err := strconv.Atoi(fields[3]); err != nil {
		return metadata.Track{}, false
	}
	if fields[4] != "" {
		return metadata.Track{}, false
	}
	if _, err := strconv.Atoi(fields[5]); err != nil {
		return metadata.Track{}, false
	}
	for _, value := range fields[6:8] {
		if _, err := time.Parse("2006-01-02T15:04:05", value); err != nil {
			return metadata.Track{}, false
		}
	}
	if strings.TrimSpace(fields[8]) == "" {
		return metadata.Track{}, false
	}
	return metadata.Track{
		RawTitle: raw,
		Artist:   strings.TrimSpace(fields[0]),
		Title:    strings.TrimSpace(fields[1]),
		Album:    strings.TrimSpace(fields[2]),
		Year:     fields[3],
		Parsed:   true,
	}, true
}
