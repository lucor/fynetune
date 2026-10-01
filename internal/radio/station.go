package radio

import (
	"errors"
	"net/url"
	"strings"
)

var (
	ErrStationNameRequired  = errors.New("station name is required")
	ErrInvalidStreamURL     = errors.New("invalid stream URL")
	ErrUnsupportedURLScheme = errors.New("stream URL must use HTTP or HTTPS")
)

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
	Language    string   `json:"language,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

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
