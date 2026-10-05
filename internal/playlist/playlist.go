// Package playlist parses the radio playlist formats supported by FyneTune.
package playlist

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// MaxBytes is the maximum size accepted for a playlist.
const MaxBytes = 512 << 10

var (
	ErrEmpty             = errors.New("playlist is empty")
	ErrNoValidEntries    = errors.New("playlist has no valid stream URLs")
	ErrUnsupportedHLS    = errors.New("HLS playlists are not supported")
	ErrRead              = errors.New("could not read radio playlist")
	ErrTooLarge          = errors.New("radio playlist exceeds size limit")
	ErrUnsupportedFormat = errors.New("unsupported playlist format")
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

// newScanner applies the same line-size limit to every playlist parser.
func newScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), MaxBytes)
	return scanner
}

// stripBOM removes a UTF-8 byte-order mark from the first playlist line.
func stripBOM(line string) string {
	return strings.TrimPrefix(line, "\xef\xbb\xbf")
}
