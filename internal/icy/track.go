package icy

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"go.lucor.dev/fynetune/internal/metadata"
)

// ParseTrackMetadata extracts track fields from the raw value of StreamTitle.
// It leaves artist and title unparsed when the separator is ambiguous.
func ParseTrackMetadata(raw string) metadata.Track {
	normalized := strings.TrimRight(strings.TrimSpace(raw), "\x00")
	normalized = strings.TrimSpace(normalized)
	track := metadata.Track{RawTitle: normalized, Title: normalized}
	if normalized == "" {
		return track
	}
	if parsed, ok := parseExtendedTildeMetadata(normalized); ok {
		return parsed
	}
	if parsed, ok := parseSongStarMetadata(normalized); ok {
		return parsed
	}

	occurrences := findSeparators(normalized)
	if len(occurrences) == 0 {
		return track
	}
	separator := occurrences[0].rune
	for _, occurrence := range occurrences[1:] {
		if occurrence.rune != separator {
			return track
		}
	}

	// A unique spaced separator disambiguates a delimiter from a bare hyphen
	// inside an artist or title, as in "AC-DC - Thunderstruck".
	selected := occurrences[0]
	var spaced separatorOccurrence
	spacedCount := 0
	for _, occurrence := range occurrences {
		if occurrence.spaced {
			spaced = occurrence
			spacedCount++
		}
	}
	if spacedCount == 1 {
		selected = spaced
	}
	if parsed, ok := splitMetadataAt(normalized, selected.index, selected.end); ok {
		parsed.RawTitle = normalized
		return parsed
	}
	return track
}

type separatorOccurrence struct {
	rune   rune
	index  int
	end    int
	spaced bool
}

var supportedSeparators = map[rune]struct{}{
	'-': {}, '–': {}, '—': {}, '~': {}, '|': {},
}

const fallbackSeparator = '*'

var uuidPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)

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

func findSeparators(value string) []separatorOccurrence {
	occurrences := make([]separatorOccurrence, 0, 2)
	fallbackOccurrences := make([]separatorOccurrence, 0, 1)
	uuidRanges := uuidPattern.FindAllStringIndex(value, -1)
	for index, separator := range value {
		_, supported := supportedSeparators[separator]
		if !supported && separator != fallbackSeparator {
			continue
		}
		end := index + utf8.RuneLen(separator)
		before, _ := utf8.DecodeLastRuneInString(value[:index])
		after, _ := utf8.DecodeRuneInString(value[end:])
		// Hyphens between digits are usually part of dates or numeric IDs in
		// structured station metadata, rather than artist/title separators.
		if separator == '-' && unicode.IsDigit(before) && unicode.IsDigit(after) {
			continue
		}
		if separator == '-' && insideUUID(index, uuidRanges) {
			continue
		}
		occurrence := separatorOccurrence{
			rune: separator, index: index, end: end,
			spaced: unicode.IsSpace(before) || unicode.IsSpace(after),
		}
		if supported {
			occurrences = append(occurrences, occurrence)
		} else if !supported {
			fallbackOccurrences = append(fallbackOccurrences, occurrence)
		}
	}
	// Asterisks are less reliable delimiters than the established separators,
	// so use them only when the title contains none of those separators.
	if len(occurrences) == 0 {
		return fallbackOccurrences
	}
	return occurrences
}

// insideUUID reports whether an index points to a separator inside a UUID.
func insideUUID(index int, ranges [][]int) bool {
	for _, r := range ranges {
		if index >= r[0] && index < r[1] {
			return true
		}
	}
	return false
}

func splitMetadataAt(value string, index, end int) (metadata.Track, bool) {
	artist := strings.TrimSpace(value[:index])
	title := strings.TrimSpace(value[end:])
	if artist == "" || title == "" {
		return metadata.Track{}, false
	}
	return metadata.Track{Artist: artist, Title: title, Parsed: true}, true
}
