package radio

import (
	"errors"
	"net/url"
	"strings"
)

var (
	// ErrStationNameRequired indicates that a station has no name.
	ErrStationNameRequired = errors.New("station name is required")
	// ErrInvalidStreamURL indicates that a station URL is not an absolute URL.
	ErrInvalidStreamURL = errors.New("invalid stream URL")
	// ErrUnsupportedURLScheme indicates that a station URL is not HTTP or HTTPS.
	ErrUnsupportedURLScheme = errors.New("stream URL must use HTTP or HTTPS")
)

// Station contains a saved or directory-provided radio station.
type Station struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	ResolvedURL string   `json:"resolved_url,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Favicon     string   `json:"favicon,omitempty"`
	Codec       string   `json:"codec,omitempty"`
	Bitrate     int      `json:"bitrate,omitempty"`
	Country     string   `json:"country,omitempty"`
	CountryCode string   `json:"country_code,omitempty"`
	Language    string   `json:"language,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ValidateStation checks that a station has a name and an HTTP or HTTPS stream URL.
func ValidateStation(s Station) error {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
	if s.Name == "" {
		return ErrStationNameRequired
	}
	u, err := url.ParseRequestURI(s.URL)
	if err != nil || u.Host == "" {
		return ErrInvalidStreamURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrUnsupportedURLScheme
	}
	return nil
}
