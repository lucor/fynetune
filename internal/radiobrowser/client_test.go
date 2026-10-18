package radiobrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/version"
)

func TestStationRecordMarksRadioBrowserSource(t *testing.T) {
	station := (stationRecord{UUID: "uuid-1", Name: "Radio", URL: "https://radio.invalid/live"}).station()
	if station.Source != radio.StationSourceRadioBrowser {
		t.Fatalf("station source = %q, want %q", station.Source, radio.StationSourceRadioBrowser)
	}
}

// TestSearchBuildsQueryAndMapsStations checks request filters and station normalization.
func TestSearchBuildsQueryAndMapsStations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/stations/search" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		for key, want := range map[string]string{"name": "jazz", "country": "Italy", "countryExact": "true", "tag": "smooth jazz", "codec": "MP3", "hidebroken": "true", "limit": "12"} {
			if got := r.URL.Query().Get(key); got != want {
				t.Errorf("query %s = %q, want %q", key, got, want)
			}
		}
		if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"stationuuid":"uuid-1","name":"Jazz FM","url":"https://playlist.example/listen.m3u","url_resolved":"https://stream.example/jazz.mp3","homepage":"https://example","favicon":"https://example/icon.png","codec":"MP3","bitrate":128,"country":"Italy","countrycode":"IT","language":"Italian","tags":"jazz, smooth jazz"}]`))
	}))
	defer server.Close()

	stations, err := NewWithBaseURL(server.URL, server.Client()).Search(context.Background(), Query{
		Name: " jazz ", Country: "Italy", CountryExact: true, Tag: "smooth jazz", Codec: "MP3", Limit: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stations) != 1 {
		t.Fatalf("got %d stations", len(stations))
	}
	got := stations[0]
	if got.ID != "uuid-1" || got.Source != radio.StationSourceRadioBrowser || got.Name != "Jazz FM" || got.URL != "https://playlist.example/listen.m3u" || got.ResolvedURL != "https://stream.example/jazz.mp3" {
		t.Fatalf("unexpected station mapping: %#v", got)
	}
	if got.Codec != "MP3" || got.Bitrate != 128 || got.Country != "Italy" || got.CountryCode != "IT" || got.Language != "Italian" {
		t.Fatalf("station metadata missing: %#v", got)
	}
	if !reflect.DeepEqual(got.Tags, []string{"jazz", "smooth jazz"}) {
		t.Fatalf("tags = %#v", got.Tags)
	}
}

// TestSearchMergesAlternativeTags checks OR-style tag searches preserve filters,
// deduplicate stations, and return alternatives in rank order.
func TestSearchMergesAlternativeTags(t *testing.T) {
	responses := map[string]string{
		"pop":    `[{"stationuuid":"a","name":"A","url":"https://radio.test/a"},{"stationuuid":"shared","name":"Shared","url":"https://radio.test/shared"}]`,
		"rock":   `[{"stationuuid":"b","name":"B","url":"https://radio.test/b"},{"stationuuid":"shared","name":"Shared","url":"https://radio.test/shared"}]`,
		"top 40": `[{"stationuuid":"c","name":"C","url":"https://radio.test/c"},{"stationuuid":"d","name":"D","url":"https://radio.test/d"}]`,
	}
	var requestedTags []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Query().Get("tag")
		requestedTags = append(requestedTags, tag)
		if got := r.URL.Query().Get("countrycode"); got != "IT" {
			t.Errorf("countrycode = %q, want IT", got)
		}
		if got := r.URL.Query().Get("codec"); got != "MP3" {
			t.Errorf("codec = %q, want MP3", got)
		}
		if got := r.URL.Query().Get("name"); got != "radio" {
			t.Errorf("name = %q, want radio", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("limit = %q, want 5", got)
		}
		if got := r.URL.Query().Get("tagExact"); got != "true" {
			t.Errorf("tagExact = %q, want true", got)
		}
		_, _ = w.Write([]byte(responses[tag]))
	}))
	defer server.Close()

	stations, err := NewWithBaseURL(server.URL, server.Client()).Search(context.Background(), Query{
		Name: "radio", CountryCode: "IT", Codec: "MP3", Tags: []string{"pop", "rock", "top 40", " POP "}, Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"pop", "rock", "top 40"}; !reflect.DeepEqual(requestedTags, want) {
		t.Fatalf("requested tags = %#v, want %#v", requestedTags, want)
	}
	var gotIDs []string
	for _, station := range stations {
		gotIDs = append(gotIDs, station.ID)
	}
	if want := []string{"a", "b", "c", "shared", "d"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("station IDs = %#v, want %#v", gotIDs, want)
	}
}

// TestCountriesReturnsNamesAndCodes checks country names and codes are returned.
func TestCountriesReturnsNamesAndCodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/countries" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"name":"Italy","iso_3166_1":"IT","stationcount":"500"},{"name":"Japan","iso_3166_1":"JP","stationcount":"200"}]`))
	}))
	defer server.Close()
	countries, err := NewWithBaseURL(server.URL, server.Client()).Countries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Country{{Name: "Italy", Code: "IT"}, {Name: "Japan", Code: "JP"}}
	if !reflect.DeepEqual(countries, want) {
		t.Fatalf("countries = %#v", countries)
	}
}

// TestSearchSupportsCountryCode checks filtering by ISO country code.
func TestSearchSupportsCountryCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("countrycode"); got != "IT" {
			t.Errorf("countrycode = %q, want IT", got)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	if _, err := NewWithBaseURL(server.URL, server.Client()).Search(context.Background(), Query{CountryCode: "IT"}); err != nil {
		t.Fatal(err)
	}
}

// TestPopularUsesTopClickEndpoint checks popular stations use the top-click endpoint.
func TestPopularUsesTopClickEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/stations/topclick" || r.URL.Query().Get("limit") != "5" {
			t.Fatalf("unexpected request %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()
	if _, err := NewWithBaseURL(server.URL, server.Client()).Popular(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPErrorIsReported checks unsuccessful API statuses are surfaced to callers.
func TestHTTPErrorIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()
	if _, err := NewWithBaseURL(server.URL, server.Client()).Popular(context.Background(), 5); err == nil {
		t.Fatal("expected HTTP error")
	}
}
