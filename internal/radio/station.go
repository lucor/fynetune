package radio

import (
	"errors"
	"net/url"
	"strings"
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
		return errors.New("station name is required")
	}
	u, err := url.ParseRequestURI(s.URL)
	if err != nil || u.Host == "" {
		return errors.New("enter a valid stream URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("stream URL must use HTTP or HTTPS")
	}
	return nil
}
