package storage

import (
	"reflect"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"go.lucor.dev/fynetune/internal/radio"
)

func TestLoadStartsWithNoStationsAndPersistsFavorites(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	s := New(a)
	v, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Stations) != 0 || v.AutoPlay || !v.Reconnect || v.Volume != .75 {
		t.Fatalf("unexpected initial settings: %+v", v)
	}
	rows := []radio.Station{{ID: "custom", Name: "Test", URL: "https://example.com/radio.mp3"}}
	if err := s.SaveStations(rows); err != nil {
		t.Fatal(err)
	}
	v, err = s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Stations) != 1 || v.Stations[0].ID != "custom" {
		t.Fatalf("stations did not persist: %+v", v.Stations)
	}
	v.DiscoveryCountry = "@IT"
	if err := s.SaveSettings(v); err != nil {
		t.Fatal(err)
	}
	v, err = s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if v.DiscoveryCountry != "@IT" {
		t.Fatalf("discovery country did not persist: %q", v.DiscoveryCountry)
	}
}

func TestLoadRemovesOnlyUnmodifiedBundledStations(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	s := New(a)
	a.Preferences().SetInt("station_defaults_version", 2)
	a.Preferences().SetString("stations", `[{"id":"custom","name":"Custom","url":"https://example.com/live"},{"id":"virgin-radio-italia","name":"Virgin Radio Italia","url":"https://icecast.unitedradio.it/Virgin.mp3","homepage":"https://www.virginradio.it/"},{"id":"rtl-1025","name":"RTL Custom","url":"https://streamingv2.shoutcast.com/rtl-1025"}]`)

	v, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Stations) != 2 || v.Stations[0].ID != "custom" || v.Stations[1].Name != "RTL Custom" {
		t.Fatalf("station cleanup did not preserve user stations: %+v", v.Stations)
	}
	if a.Preferences().Int("station_defaults_version") != stationMigrationVersion {
		t.Fatal("station migration version was not persisted")
	}
}

func TestInvalidStationsJSONPreservesStoredValue(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	s := New(a)
	a.Preferences().SetString("stations", "{broken")
	if _, err := s.Load(); err == nil {
		t.Fatal("expected corrupt JSON error")
	}
	if got := a.Preferences().String("stations"); got != "{broken" {
		t.Fatalf("corrupt value overwritten: %q", got)
	}
}

func TestRecentStationsPersistAcrossStoreLoads(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	store := New(a)
	want := []RecentStation{{
		Station:  radio.Station{ID: "directory-station", Name: "Jazz FM", URL: "https://example.com/jazz.mp3"},
		PlayedAt: time.Date(2026, time.September, 25, 10, 30, 0, 0, time.UTC),
	}}
	if err := store.SaveRecent(want); err != nil {
		t.Fatal(err)
	}

	got, err := New(a).LoadRecent()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recent stations = %#v, want %#v", got, want)
	}
}

func TestInvalidRecentJSONIsPreserved(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Preferences().SetString(recentStationsPreference, "{broken")

	if _, err := New(a).LoadRecent(); err == nil {
		t.Fatal("expected corrupt recent stations JSON error")
	}
	if got := a.Preferences().String(recentStationsPreference); got != "{broken" {
		t.Fatalf("corrupt recent stations value overwritten: %q", got)
	}
}
