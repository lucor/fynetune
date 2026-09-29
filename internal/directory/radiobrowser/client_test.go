package radiobrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"go.lucor.dev/fynetune/internal/version"
)

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
		_, _ = w.Write([]byte(`[{"stationuuid":"uuid-1","name":"Jazz FM","url":"https://playlist.example/listen.m3u","url_resolved":"https://stream.example/jazz.mp3","homepage":"https://example","favicon":"https://example/icon.png","codec":"MP3","bitrate":128,"country":"Italy","language":"Italian","tags":"jazz, smooth jazz"}]`))
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
	if got.ID != "uuid-1" || got.Name != "Jazz FM" || got.URL != "https://playlist.example/listen.m3u" || got.ResolvedURL != "https://stream.example/jazz.mp3" {
		t.Fatalf("unexpected station mapping: %#v", got)
	}
	if got.Codec != "MP3" || got.Bitrate != 128 || got.Country != "Italy" || got.Language != "Italian" {
		t.Fatalf("station metadata missing: %#v", got)
	}
	if !reflect.DeepEqual(got.Tags, []string{"jazz", "smooth jazz"}) {
		t.Fatalf("tags = %#v", got.Tags)
	}
}

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

func TestHTTPErrorIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()
	if _, err := NewWithBaseURL(server.URL, server.Client()).Popular(context.Background(), 5); err == nil {
		t.Fatal("expected HTTP error")
	}
}
