package playlist

import (
	"fmt"
	"io"
	"strings"
)

// ParseM3U parses an M3U or UTF-8 M3U8 playlist. HLS manifests are rejected.
func ParseM3U(r io.Reader) (Playlist, error) {
	scanner := newScanner(r)
	var result Playlist
	var title string
	seen := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if lineNumber == 1 {
			line = stripBOM(line)
		}
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		// EXT-X tags identify an HLS manifest rather than a station playlist.
		if strings.HasPrefix(upper, "#EXT-X-") {
			return Playlist{}, ErrUnsupportedHLS
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(upper, "#EXTINF:") {
				_, value, ok := strings.Cut(line, ",")
				if ok {
					title = strings.TrimSpace(value)
				} else {
					title = ""
				}
			}
			continue
		}
		result.Entries = append(result.Entries, Entry{URL: line, Title: title})
		title = ""
		seen = true
	}
	if err := scanner.Err(); err != nil {
		return Playlist{}, fmt.Errorf("scan M3U playlist: %w", err)
	}
	if !seen {
		return Playlist{}, ErrEmpty
	}
	return result, nil
}
