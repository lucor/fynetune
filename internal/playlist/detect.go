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

const MaxBytes = 512 << 10

type Kind uint8

const (
	KindNotPlaylist Kind = iota
	KindM3U
	KindPLS
	KindUnsupported
)

// Detect identifies a playlist from its response type, URL, or content prefix.
func Detect(contentType, rawURL string, prefix []byte) Kind {
	if isPlaylist(contentType, rawURL) {
		return format(contentType, rawURL, prefix)
	}
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf"))
	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "[PLAYLIST]") {
		return KindPLS
	}
	if strings.HasPrefix(upper, "#EXTM3U") || strings.HasPrefix(upper, "#EXTINF") {
		return KindM3U
	}
	return KindNotPlaylist
}

// Parse reads and parses a detected playlist, enforcing the playlist size limit.
func Parse(reader io.Reader, kind Kind) (Playlist, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return Playlist{}, fmt.Errorf("%w: %w", ErrRead, err)
	}
	if len(data) > MaxBytes {
		return Playlist{}, ErrTooLarge
	}
	if kind == KindPLS {
		return ParsePLS(bytes.NewReader(data))
	}
	return ParseM3U(bytes.NewReader(data))
}

func format(contentType, rawURL string, prefix []byte) Kind {
	trimmed := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(string(prefix), "\xef\xbb\xbf")))
	if strings.HasPrefix(trimmed, "[PLAYLIST]") {
		return KindPLS
	}
	if strings.HasPrefix(trimmed, "#EXTM3U") || strings.HasPrefix(trimmed, "#EXTINF") {
		return KindM3U
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch strings.ToLower(mediaType) {
	case "audio/x-scpls":
		return KindPLS
	case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl":
		return KindM3U
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		switch strings.ToLower(path.Ext(parsed.Path)) {
		case ".pls":
			return KindPLS
		case ".m3u", ".m3u8":
			return KindM3U
		case ".xspf", ".asx", ".wpl":
			return KindUnsupported
		}
	}
	if strings.EqualFold(mediaType, "application/pls+xml") || strings.EqualFold(mediaType, "application/xspf+xml") ||
		strings.EqualFold(mediaType, "video/x-ms-asf") || strings.EqualFold(mediaType, "application/vnd.ms-asf") ||
		strings.EqualFold(mediaType, "application/x-mplayer2") || strings.EqualFold(mediaType, "application/x-ms-wpl") {
		return KindUnsupported
	}
	return KindM3U
}

func isPlaylist(contentType, rawURL string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil {
		switch strings.ToLower(mediaType) {
		case "application/vnd.apple.mpegurl", "application/x-mpegurl", "audio/mpegurl", "audio/x-mpegurl",
			"audio/x-scpls", "application/pls+xml", "application/xspf+xml", "video/x-ms-asf",
			"application/vnd.ms-asf", "application/x-mplayer2", "application/x-ms-wpl":
			return true
		}
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(path.Ext(parsed.Path)) {
	case ".m3u", ".m3u8", ".pls", ".xspf", ".asx", ".wpl":
		return true
	default:
		return false
	}
}
