package radio

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// StationSource identifies where a station was added.
type StationSource string

const (
	// StationSourceRadioBrowser marks stations provided by Radio Browser.
	StationSourceRadioBrowser StationSource = "radiobrowser"
	StationSourceCustom       StationSource = "custom"
)

var (
	// ErrStationNameRequired indicates that a station has no name.
	ErrStationNameRequired = errors.New("station name is required")
	// ErrInvalidStreamURL indicates that a station URL is not an absolute URL.
	ErrInvalidStreamURL = errors.New("invalid stream URL")
	// ErrUnsupportedURLScheme indicates that a station URL is not HTTP or HTTPS.
	ErrUnsupportedURLScheme = errors.New("stream URL must use HTTP or HTTPS")
	// ErrInvalidWebsiteURL indicates that an optional website is not an HTTP or HTTPS URL.
	ErrInvalidWebsiteURL = errors.New("website must be a valid HTTP or HTTPS URL")
	// ErrDuplicateCustomStation indicates that another custom station uses the stream URL.
	ErrDuplicateCustomStation = errors.New("a custom station with this stream URL already exists")
	// ErrNotCustomStation indicates that an operation requires a custom station.
	ErrNotCustomStation = errors.New("station is not a custom station")
)

// Station contains a saved or directory-provided radio station.
type Station struct {
	ID          string        `json:"id"`
	Source      StationSource `json:"source,omitempty"`
	Name        string        `json:"name"`
	URL         string        `json:"url"`
	ResolvedURL string        `json:"resolved_url,omitempty"`
	Homepage    string        `json:"homepage,omitempty"`
	Favicon     string        `json:"favicon,omitempty"`
	Codec       string        `json:"codec,omitempty"`
	Bitrate     int           `json:"bitrate,omitempty"`
	Country     string        `json:"country,omitempty"`
	CountryCode string        `json:"country_code,omitempty"`
	Language    string        `json:"language,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
}

// IsCustom reports whether a station was entered by the user.
func (s Station) IsCustom() bool { return s.Source == StationSourceCustom }

// NewCustomStation validates and creates a custom station with a stable local ID.
func NewCustomStation(name, streamURL, website string, favorites []Station) (Station, error) {
	station := Station{
		Source:   StationSourceCustom,
		Name:     strings.TrimSpace(name),
		URL:      strings.TrimSpace(streamURL),
		Homepage: strings.TrimSpace(website),
	}
	if err := ValidateCustomStation(station, favorites); err != nil {
		return Station{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Station{}, fmt.Errorf("generate station ID: %w", err)
	}
	station.ID = id.String()
	return station, nil
}

// UpdateCustomStation validates replacement details while preserving station identity.
func UpdateCustomStation(existing Station, name, streamURL, website string, favorites []Station) (Station, error) {
	if !existing.IsCustom() {
		return Station{}, ErrNotCustomStation
	}
	updated := existing
	updated.Name = strings.TrimSpace(name)
	updated.URL = strings.TrimSpace(streamURL)
	updated.Homepage = strings.TrimSpace(website)
	if updated.URL != existing.URL {
		updated.ResolvedURL = ""
	}
	if err := ValidateCustomStation(updated, favorites); err != nil {
		return Station{}, err
	}
	return updated, nil
}

// ValidateCustomStation validates editable fields without contacting the station.
// Only other custom stations are checked for duplicate URLs; directory favorites
// retain their own identities even when they use the same stream URL.
func ValidateCustomStation(station Station, favorites []Station) error {
	if err := ValidateStation(station); err != nil {
		return err
	}
	website := strings.TrimSpace(station.Homepage)
	if website != "" {
		u, err := url.Parse(website)
		if err != nil || u.Hostname() == "" || strings.ContainsAny(website, " \t\r\n") || !isHTTPScheme(u.Scheme) {
			return ErrInvalidWebsiteURL
		}
	}
	for _, existing := range favorites {
		if existing.IsCustom() && (station.ID == "" || existing.ID != station.ID) &&
			strings.TrimSpace(existing.URL) == strings.TrimSpace(station.URL) {
			return ErrDuplicateCustomStation
		}
	}
	return nil
}

// ValidateStation checks that a station has a name and an HTTP or HTTPS stream URL.
func ValidateStation(s Station) error {
	s.Name = strings.TrimSpace(s.Name)
	s.URL = strings.TrimSpace(s.URL)
	if s.Name == "" {
		return ErrStationNameRequired
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Scheme == "" || u.Hostname() == "" || strings.ContainsAny(s.URL, " \t\r\n") {
		return ErrInvalidStreamURL
	}
	if !isSupportedScheme(u) {
		return ErrUnsupportedURLScheme
	}
	return nil
}

// isSupportedScheme reports whether the stream URL uses a scheme supported by
// the playback transport.
func isSupportedScheme(u *url.URL) bool {
	return u != nil && isHTTPScheme(u.Scheme)
}

func isHTTPScheme(scheme string) bool {
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}
