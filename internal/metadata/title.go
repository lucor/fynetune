package metadata

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParseTitle extracts artist and title from unstructured source text.
// It preserves the normalized original text and leaves ambiguous titles unsplit.
func ParseTitle(raw string) Track {
	normalized := strings.TrimRight(strings.TrimSpace(raw), "\x00")
	normalized = strings.TrimSpace(normalized)
	track := Track{RawTitle: normalized, Title: normalized}
	if normalized == "" {
		return track
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

func splitMetadataAt(value string, index, end int) (Track, bool) {
	artist := strings.TrimSpace(value[:index])
	title := strings.TrimSpace(value[end:])
	if artist == "" || title == "" {
		return Track{}, false
	}
	return Track{Artist: artist, Title: title, Parsed: true}, true
}
