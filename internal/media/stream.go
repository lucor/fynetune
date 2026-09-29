// Package media contains transport-independent audio stream metadata.
package media

import (
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type TrackMetadata struct {
	RawTitle string
	Artist   string
	Title    string
	Album    string
	Year     string
	Parsed   bool
}

// String returns the available track fields in display order.
func (m TrackMetadata) String() string {
	parts := make([]string, 0, 4)
	for _, field := range []string{m.Artist, m.Title, m.Album, m.Year} {
		if field = strings.TrimSpace(field); field != "" {
			parts = append(parts, field)
		}
	}
	if len(parts) == 0 {
		return m.RawTitle
	}
	return strings.Join(parts, " - ")
}

func ParseTrackMetadata(raw string) TrackMetadata {
	return NewTrackMetadataParser().Parse(raw)
}

const (
	initialLearningWindow = 4
	separatorWindowSize   = 8
)

// TrackMetadataParser learns one station's preferred artist/title separator.
// Callers should keep one parser per station and synchronize concurrent calls.
type TrackMetadataParser struct {
	lastTitle string
	preferred rune
	observed  []rune
	totalSeen int
}

func NewTrackMetadataParser() *TrackMetadataParser {
	return &TrackMetadataParser{observed: make([]rune, 0, separatorWindowSize)}
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

func (p *TrackMetadataParser) Parse(raw string) TrackMetadata {
	normalized := strings.TrimRight(strings.TrimSpace(raw), "\x00")
	normalized = strings.TrimSpace(normalized)
	metadata := TrackMetadata{RawTitle: normalized, Title: normalized}
	if normalized == "" {
		return metadata
	}
	duplicate := normalized == p.lastTitle
	p.lastTitle = normalized
	occurrences := findSeparators(normalized)
	distinct := make(map[rune]struct{}, len(occurrences))
	for _, occurrence := range occurrences {
		distinct[occurrence.rune] = struct{}{}
	}

	if !duplicate && len(distinct) == 1 {
		for separator := range distinct {
			p.observe(separator)
		}
	}
	if parsed, ok := parseExtendedTildeMetadata(normalized); ok {
		return parsed
	}

	if p.preferred != 0 {
		if parsed, ok := splitMetadata(normalized, p.preferred); ok {
			parsed.RawTitle = normalized
			return parsed
		}
		return metadata
	}

	// Repeated occurrences of one delimiter type are still unambiguous. They
	// commonly represent a station's structured ICY fields (for example,
	// "Artist~Title~Album~..."). Use the first occurrence provisionally; the
	// whole metadata block contributes only one learning observation above.
	if len(distinct) == 1 && len(occurrences) > 0 {
		if parsed, ok := splitMetadataAt(normalized, occurrences[0].index, occurrences[0].end); ok {
			parsed.RawTitle = normalized
			return parsed
		}
		return metadata
	}

	// A single spaced separator can disambiguate a delimiter from a bare
	// hyphen in a name, such as "AC-DC - Thunderstruck".
	if len(distinct) == 1 {
		var candidate *separatorOccurrence
		for i := range occurrences {
			if !occurrences[i].spaced {
				continue
			}
			if candidate != nil {
				return metadata
			}
			candidate = &occurrences[i]
		}
		if candidate != nil {
			if parsed, ok := splitMetadataAt(normalized, candidate.index, candidate.end); ok {
				parsed.RawTitle = normalized
				return parsed
			}
		}
	}
	return metadata
}

// parseExtendedTildeMetadata recognizes the extended fields used by some
// broadcasters: artist, title, album, year, duration, two timestamps, and
// station. The track, album, and year are kept as separate display fields.
func parseExtendedTildeMetadata(raw string) (TrackMetadata, bool) {
	fields := strings.Split(raw, "~")
	if len(fields) < 9 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
		return TrackMetadata{}, false
	}
	if len(fields[3]) != 4 {
		return TrackMetadata{}, false
	}
	if _, err := strconv.Atoi(fields[3]); err != nil {
		return TrackMetadata{}, false
	}
	if fields[4] != "" {
		return TrackMetadata{}, false
	}
	if _, err := strconv.Atoi(fields[5]); err != nil {
		return TrackMetadata{}, false
	}
	for _, value := range fields[6:8] {
		if _, err := time.Parse("2006-01-02T15:04:05", value); err != nil {
			return TrackMetadata{}, false
		}
	}
	if strings.TrimSpace(fields[8]) == "" {
		return TrackMetadata{}, false
	}
	return TrackMetadata{
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
	for index, separator := range value {
		if _, ok := supportedSeparators[separator]; !ok {
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
		occurrences = append(occurrences, separatorOccurrence{
			rune: separator, index: index, end: end,
			spaced: unicode.IsSpace(before) || unicode.IsSpace(after),
		})
	}
	return occurrences
}

func splitMetadata(value string, separator rune) (TrackMetadata, bool) {
	occurrences := findSeparators(value)
	firstIndex, firstEnd := -1, -1
	var spaced *separatorOccurrence
	spacedCount := 0
	for i := range occurrences {
		occurrence := &occurrences[i]
		if occurrence.rune != separator {
			continue
		}
		if firstIndex < 0 {
			firstIndex, firstEnd = occurrence.index, occurrence.end
		}
		if occurrence.spaced {
			spaced = occurrence
			spacedCount++
		}
	}
	if firstIndex < 0 {
		return TrackMetadata{}, false
	}
	// If a bare hyphen appears inside a name, a unique spaced occurrence is
	// the better delimiter while preserving first-occurrence splitting otherwise.
	if spacedCount == 1 {
		return splitMetadataAt(value, spaced.index, spaced.end)
	}
	return splitMetadataAt(value, firstIndex, firstEnd)
}

func splitMetadataAt(value string, index, end int) (TrackMetadata, bool) {
	artist := strings.TrimSpace(value[:index])
	title := strings.TrimSpace(value[end:])
	if artist == "" || title == "" {
		return TrackMetadata{}, false
	}
	return TrackMetadata{Artist: artist, Title: title, Parsed: true}, true
}

func (p *TrackMetadataParser) observe(separator rune) {
	if p.totalSeen < initialLearningWindow {
		p.totalSeen++
	}
	p.observed = append(p.observed, separator)
	if len(p.observed) > separatorWindowSize {
		p.observed = p.observed[len(p.observed)-separatorWindowSize:]
	}
	if p.totalSeen < initialLearningWindow {
		return
	}

	counts := make(map[rune]int, len(supportedSeparators))
	for _, observed := range p.observed {
		counts[observed]++
	}
	winner, most, runnerUp := rune(0), 0, 0
	for separator, count := range counts {
		if count > most {
			runnerUp = most
			winner, most = separator, count
		} else if count > runnerUp {
			runnerUp = count
		}
	}
	if most >= 3 && most > runnerUp {
		p.preferred = winner
	}
}

type StreamInfo struct {
	URL      string
	Codec    string
	MIMEType string
}
