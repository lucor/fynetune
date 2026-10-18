package id3

import (
	"sort"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/metadata"
)

// Track normalizes the musical fields of one tag without retaining prior state.
// TPE1, TIT2, and TALB provide artist, title, and album. Year uses TDRL, TDRC,
// then TYER. Multiple artists are joined with a comma. If TIT2 is absent, TIT1
// or a TXXX description named grouping supplies generic artist/title text.
// Standard fields take precedence over that fallback. TEXT remains opaque.
// IsUpdate is not applied here: a caller handling partial tags must first merge
// their source fields. Debug logs show decoded input, output, and ignored fields.
func (t Tag) Track() metadata.Track {
	used := make(map[string]bool)
	field := func(id string) string {
		for _, value := range t.TextFrames[id] {
			if text := cleanText(value); text != "" {
				used[id] = true
				return text
			}
		}
		return ""
	}
	var artists []string
	for _, value := range t.TextFrames["TPE1"] {
		if text := cleanText(value); text != "" {
			artists = append(artists, text)
		}
	}
	used["TPE1"] = len(artists) > 0
	track := metadata.Track{Artist: strings.Join(artists, ", "), Title: field("TIT2"), Album: field("TALB")}
	for _, id := range []string{"TDRL", "TDRC", "TYER"} {
		for _, value := range t.TextFrames[id] {
			if year := dateYear(cleanText(value)); year != "" {
				track.Year = year
				used[id] = true
				break
			}
		}
		if track.Year != "" {
			break
		}
	}
	if track.Title == "" {
		grouping := field("TIT1")
		if grouping == "" {
			// Sorting makes case variants deterministic without rewriting keys.
			keys := make([]string, 0, len(t.UserTextFrames))
			for key := range t.UserTextFrames {
				if strings.EqualFold(key, "grouping") {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				for _, value := range t.UserTextFrames[key] {
					if grouping = cleanText(value); grouping != "" {
						used["TXXX:"+key] = true
						break
					}
				}
				if grouping != "" {
					break
				}
			}
		}
		if grouping != "" {
			fallback := metadata.ParseTitle(grouping)
			track.RawTitle = fallback.RawTitle
			if track.Artist == "" || track.Artist == fallback.Artist {
				track.Title = fallback.Title
				if track.Artist == "" {
					track.Artist = fallback.Artist
				}
			} else {
				track.Title = grouping
			}
		}
	}
	track.Parsed = track.Artist != "" && track.Title != ""
	metadata.DebugNormalization(metadata.SourceID3, t, track, t.ignoredFields(used))
	return track
}

// ignoredFields describes source fields not selected for the normalized track.
func (t Tag) ignoredFields(used map[string]bool) map[string]string {
	ignored := make(map[string]string)
	for id := range t.TextFrames {
		if used[id] {
			continue
		}
		switch id {
		case "TEXT":
			ignored[id] = "text writer field; payload is not interpreted"
		case "TDRL", "TDRC", "TYER":
			ignored[id] = "invalid, empty, or lower-priority date"
		case "TIT1":
			ignored[id] = "empty grouping or standard title takes precedence"
		default:
			ignored[id] = "empty or not mapped to track fields"
		}
	}
	for description := range t.UserTextFrames {
		key := "TXXX:" + description
		if !used[key] {
			ignored[key] = "empty, not mapped, or standard fields take precedence"
		}
	}
	return ignored
}

// cleanText removes surrounding whitespace and trailing null padding.
func cleanText(value string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(value), "\x00"))
}

// dateYear validates complete or partial ISO dates before extracting the year.
func dateYear(value string) string {
	for _, layout := range []string{"2006", "2006-01", "2006-01-02", "2006-01-02T15", "2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339Nano} {
		if _, err := time.Parse(layout, value); err == nil {
			return value[:4]
		}
	}
	return ""
}
