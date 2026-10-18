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

func TestCustomStationsPersistWithLegacyFavoritesAndRecent(t *testing.T) {
	app := test.NewTempApp(t)
	preferences := app.Preferences()
	preferences.SetString("stations", `[{"id":"directory-station","name":"Directory radio","url":"https://radio.invalid/live","resolved_url":"https://radio.invalid/resolved","homepage":"https://example.com","codec":"MP3","country_code":"IT"}]`)
	preferences.SetInt("station_defaults_version", stationMigrationVersion)
	store := New(app)
	settings, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	legacy := settings.Stations[0]
	if legacy.Source != "" || legacy.IsCustom() {
		t.Fatal("a saved station without a source must retain directory behavior")
	}
	custom, err := radio.NewCustomStation("Custom radio", legacy.URL, "https://custom.invalid", settings.Stations)
	if err != nil {
		t.Fatal(err)
	}
	settings.Stations = append(settings.Stations, custom)
	settings.Selected = custom.ID
	if err := store.SaveStations(settings.Stations); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	recent := []RecentStation{
		{Station: custom, PlayedAt: time.Date(2026, time.October, 19, 10, 0, 0, 0, time.UTC)},
		{Station: legacy, PlayedAt: time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)},
	}
	if err := store.SaveRecent(recent); err != nil {
		t.Fatal(err)
	}

	// Recreate the app with only the persisted values, without sharing station objects.
	restartedApp := test.NewTempApp(t)
	for _, key := range []string{"stations", "selected_station_id", recentStationsPreference} {
		restartedApp.Preferences().SetString(key, preferences.String(key))
	}
	restartedApp.Preferences().SetInt("station_defaults_version", preferences.Int("station_defaults_version"))
	restarted := New(restartedApp)
	loaded, err := restarted.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Stations, settings.Stations) || loaded.Selected != custom.ID {
		t.Fatalf("stations or selection changed after reload: %+v", loaded)
	}
	loadedRecent, err := restarted.LoadRecent()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loadedRecent, recent) {
		t.Fatalf("recent stations changed after reload: %+v", loadedRecent)
	}
	if len(loaded.Stations) != 2 || !loaded.Stations[1].IsCustom() {
		t.Fatal("custom and directory favorites sharing a URL must remain separate")
	}
	if restartedApp.Preferences().Int("station_defaults_version") != stationMigrationVersion {
		t.Fatal("custom stations must not require a new station migration")
	}
}

func TestCustomStationURLChangePreservesSavedSelection(t *testing.T) {
	app := test.NewTempApp(t)
	store := New(app)
	station, err := radio.NewCustomStation("Custom", "https://radio.invalid/old", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStations([]radio.Station{station}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(Settings{Selected: station.ID}); err != nil {
		t.Fatal(err)
	}
	settings, err := New(app).Load()
	if err != nil {
		t.Fatal(err)
	}
	updated, err := radio.UpdateCustomStation(settings.Stations[0], "Updated", "https://radio.invalid/new", "", settings.Stations)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStations([]radio.Station{updated}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(app).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Stations) != 1 || reloaded.Stations[0].ID != station.ID || reloaded.Selected != station.ID || reloaded.Stations[0].URL != updated.URL || !reloaded.Stations[0].IsCustom() {
		t.Fatalf("URL edit changed identity or selection: %+v", reloaded)
	}
}
