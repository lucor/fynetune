package playlist

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
)

type Kind uint8

const (
	KindNotPlaylist Kind = iota
	KindM3U
	KindPLS
	KindUnsupported
)

// Detect identifies a playlist from its response type, URL, or content prefix.
func Detect(contentType, rawURL string, prefix []byte) Kind {
	if kind := kindFromPrefix(prefix); kind != KindNotPlaylist {
		return kind
	}
	return kindFromResponse(contentType, rawURL)
}

// Parse reads and parses a detected playlist, enforcing the playlist size limit.
func Parse(reader io.Reader, kind Kind) (Playlist, error) {
	if kind != KindM3U && kind != KindPLS {
		return Playlist{}, ErrUnsupportedFormat
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Playlist{}, fmt.Errorf("%w: %w", ErrRead, err)
	}
	if len(data) > MaxBytes {
		return Playlist{}, ErrTooLarge
	}
	switch kind {
	case KindPLS:
		return ParsePLS(bytes.NewReader(data))
	case KindM3U:
		return ParseM3U(bytes.NewReader(data))
	default:
		return Playlist{}, ErrUnsupportedFormat
	}
}

func kindFromPrefix(prefix []byte) Kind {
	trimmed := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf")))
	if strings.HasPrefix(trimmed, "[PLAYLIST]") {
		return KindPLS
	}
	if strings.HasPrefix(trimmed, "#EXTM3U") || strings.HasPrefix(trimmed, "#EXTINF") {
		return KindM3U
	}
	return KindNotPlaylist
}

func kindFromResponse(contentType, rawURL string) Kind {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	mediaKind := KindNotPlaylist
	switch strings.ToLower(mediaType) {
	case "audio/x-scpls":
		return KindPLS
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return KindM3U
	case "application/pls+xml", "application/xspf+xml", "video/x-ms-asf",
		"application/vnd.ms-asf", "application/x-mplayer2", "application/x-ms-wpl":
		mediaKind = KindUnsupported
	}
	if kind := kindFromURL(rawURL); kind != KindNotPlaylist {
		return kind
	}
	return mediaKind
}

func kindFromURL(rawURL string) Kind {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return KindNotPlaylist
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".pls":
		return KindPLS
	case ".m3u", ".m3u8":
		return KindM3U
	case ".xspf", ".asx", ".wpl":
		return KindUnsupported
	default:
		return KindNotPlaylist
	}
}
