package radio

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestNewCustomStationUsesIndependentV7Identity(t *testing.T) {
	first, err := NewCustomStation("  Offline radio \t", " https://offline.invalid/live?token=abc ", " https://example.com ", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !first.IsCustom() || first.Name != "Offline radio" || first.URL != "https://offline.invalid/live?token=abc" || first.Homepage != "https://example.com" {
		t.Fatalf("unexpected custom station: %+v", first)
	}
	id, err := uuid.Parse(first.ID)
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		t.Fatalf("station ID %q must be an RFC UUIDv7: %v", first.ID, err)
	}
	second, err := NewCustomStation(first.Name, first.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatal("separate creations must have distinct identities even with identical URLs")
	}
}

func TestNewCustomStationValidatesUserFields(t *testing.T) {
	for _, tc := range []struct {
		name, streamURL, website string
		want                     error
	}{
		{name: "", streamURL: "https://radio.invalid/live", want: ErrStationNameRequired},
		{name: " \t\n ", streamURL: "https://radio.invalid/live", want: ErrStationNameRequired},
		{name: "Radio", streamURL: "", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: " \t ", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: "radio.invalid/live", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: "https:///live", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: "https://:8000/live", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: "https://radio.invalid:bad/live", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: "https://radio.invalid/a b", want: ErrInvalidStreamURL},
		{name: "Radio", streamURL: "ftp://radio.invalid/live", want: ErrUnsupportedURLScheme},
		{name: "Radio", streamURL: "wss://radio.invalid/live", want: ErrUnsupportedURLScheme},
		{name: "Radio", streamURL: "http://radio.invalid:8000/live"},
		{name: "Radio", streamURL: "https://radio.invalid/listen.m3u8?token=a%2Fb", website: " \t "},
		{name: "Radio", streamURL: "https://[::1]:8000/live", website: "http://example.com"},
		{name: "Radio", streamURL: "https://radio.invalid/live", website: "example.com", want: ErrInvalidWebsiteURL},
		{name: "Radio", streamURL: "https://radio.invalid/live", website: "javascript:alert(1)", want: ErrInvalidWebsiteURL},
		{name: "Radio", streamURL: "https://radio.invalid/live", website: "https://:443", want: ErrInvalidWebsiteURL},
		{name: "Radio", streamURL: "https://radio.invalid/live", website: "https://example.com/a b", want: ErrInvalidWebsiteURL},
	} {
		t.Run(tc.name+"/"+tc.streamURL+"/"+tc.website, func(t *testing.T) {
			station, err := NewCustomStation(tc.name, tc.streamURL, tc.website, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if err != nil && !reflect.DeepEqual(station, Station{}) {
				t.Fatalf("invalid input returned a station: %+v", station)
			}
		})
	}
}

func TestCustomStationDuplicateRules(t *testing.T) {
	custom, err := NewCustomStation("Custom", "https://radio.invalid/live?token=one", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	favorites := []Station{custom}
	if _, err := NewCustomStation("Another name", " https://radio.invalid/live?token=one ", "", favorites); !errors.Is(err, ErrDuplicateCustomStation) {
		t.Fatalf("same custom URL error = %v, want duplicate", err)
	}
	if _, err := NewCustomStation("Different stream", "https://radio.invalid/live?token=two", "", favorites); err != nil {
		t.Fatalf("different query parameters must remain distinct: %v", err)
	}
	directory := Station{ID: "directory", Name: "Directory", URL: custom.URL}
	if _, err := NewCustomStation("Custom", directory.URL, "", []Station{directory}); err != nil {
		t.Fatalf("a directory favorite must not prevent a separate custom station: %v", err)
	}
	if _, err := UpdateCustomStation(custom, "Renamed", custom.URL, "", favorites); err != nil {
		t.Fatalf("editing a station must exclude its own ID from duplicate checks: %v", err)
	}
	other, err := NewCustomStation("Other", "https://radio.invalid/other", "", favorites)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateCustomStation(other, other.Name, custom.URL, "", append(favorites, other)); !errors.Is(err, ErrDuplicateCustomStation) {
		t.Fatalf("editing to another custom URL error = %v, want duplicate", err)
	}
}

func TestUpdateCustomStationPreservesIdentity(t *testing.T) {
	original, err := NewCustomStation("Original", "https://radio.invalid/old", "https://old.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	original.ResolvedURL = "https://resolved.invalid/old"
	updated, err := UpdateCustomStation(original, " New name ", " https://radio.invalid/new ", " ", []Station{original})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Source != original.Source || updated.Name != "New name" || updated.URL != "https://radio.invalid/new" || updated.Homepage != "" || updated.ResolvedURL != "" {
		t.Fatalf("unexpected station update: %+v", updated)
	}
	if original.Name != "Original" || original.URL != "https://radio.invalid/old" {
		t.Fatal("updating must not mutate the original station")
	}
	if _, err := UpdateCustomStation(original, " ", updated.URL, "", nil); !errors.Is(err, ErrStationNameRequired) {
		t.Fatalf("invalid edit error = %v, want name required", err)
	}
	if _, err := UpdateCustomStation(Station{ID: "directory"}, "Name", updated.URL, "", nil); !errors.Is(err, ErrNotCustomStation) {
		t.Fatalf("directory edit error = %v, want not custom", err)
	}
}
