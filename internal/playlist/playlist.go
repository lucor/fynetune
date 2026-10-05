// Package playlist parses the radio playlist formats supported by FyneTune.
package playlist

import "errors"

var (
	ErrEmpty          = errors.New("playlist is empty")
	ErrNoValidEntries = errors.New("playlist has no valid stream URLs")
	ErrUnsupportedHLS = errors.New("HLS playlists are not supported")
	ErrRead           = errors.New("could not read radio playlist")
	ErrTooLarge       = errors.New("radio playlist exceeds size limit")
)

// Entry is a stream URL and its optional display title.
type Entry struct {
	URL   string
	Title string
}

// Playlist contains entries in playback order.
type Playlist struct {
	Entries []Entry
}
