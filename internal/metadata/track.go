// Package metadata defines the musical information shared by stream metadata sources.
package metadata

import "strings"

// Track contains the available musical fields for the current track.
// Sources may provide structured fields or only an unstructured title.
type Track struct {
	RawTitle string // Unstructured title supplied by the source, when available.
	Artist   string // Artist, when available.
	Title    string // Track title, or the unstructured title when it cannot be split.
	Album    string // Album, when available.
	Year     string // Release year, when available.
	Parsed   bool   // Whether artist and title were identified separately.
}

// String returns available track fields in display order.
func (m Track) String() string {
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
