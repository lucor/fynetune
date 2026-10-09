package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
)

// TestRecentGroupsGlobalHistoryAndPlaysStation checks native day headings and
// station taps, including history from outside the discovery country.
func TestRecentGroupsGlobalHistoryAndPlaysStation(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, widget.NewLabel(""))
	player := &trayTestPlayer{}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 14, 37, 0, 0, now.Location())
	italian := radio.Station{ID: "it", Name: "Virgin Radio Italia", CountryCode: "IT", Codec: "MP3", Bitrate: 128}
	american := radio.Station{ID: "us", Name: "Radio Paradise", CountryCode: "US", Codec: "AAC", Bitrate: 192}
	yesterday := radio.Station{ID: "yesterday", Name: "Rai Radio 1", CountryCode: "IT"}
	w := &Window{
		app: app, window: window, store: storage.New(app), radioPlayer: player,
		settings: storage.Settings{DiscoveryCountry: "@IT"},
		recent: []storage.RecentStation{
			{Station: yesterday, PlayedAt: today.AddDate(0, 0, -1)},
			{Station: american, PlayedAt: today.Add(-time.Hour)},
			{Station: italian, PlayedAt: today},
		},
	}
	w.playerBar = newPlayerWidget(app.Settings().Theme(), .75, nil, nil, nil)
	w.recentSection = newRecentSection(w)
	rows := w.recentSection.rows
	window.SetContent(w.recentSection.Container)
	window.Resize(fyne.NewSize(480, 420))
	window.Show()

	history := w.recentSection.history
	radioRow := newStationList(w, false).newRow()
	if got, want := history.Objects[1].MinSize().Height, radioRow.MinSize().Height; got != want {
		t.Fatalf("recent row height = %v, want Radio/Favorites height %v", got, want)
	}
	if len(rows.entries) != 3 || len(history.Objects) != 5 {
		t.Fatal("history must show three stations under two day headings")
	}
	for _, scenario := range []struct {
		index int
		text  string
	}{{0, "Today"}, {3, "Yesterday"}} {
		heading, ok := history.Objects[scenario.index].(*widget.Label)
		if !ok || heading.Text != scenario.text {
			t.Fatalf("history heading %d must be a plain %q label", scenario.index, scenario.text)
		}
		if _, ok := history.Objects[scenario.index].(desktop.Hoverable); ok {
			t.Fatal("day headings must not respond to hover")
		}
		if _, ok := history.Objects[scenario.index].(fyne.Tappable); ok {
			t.Fatal("day headings must not be tappable")
		}
	}
	if rows.entries[0].station.ID != italian.ID || rows.entries[1].station.ID != american.ID || rows.entries[2].station.ID != yesterday.ID {
		t.Fatal("history must include stations from every country, newest first")
	}
	headingPos := app.Driver().AbsolutePositionForObject(history.Objects[0])
	test.TapCanvas(window.Canvas(), headingPos.Add(fyne.NewPos(12, 12)))
	if player.played.ID != "" {
		t.Fatal("selecting a day heading must not start playback")
	}
	stationPos := app.Driver().AbsolutePositionForObject(history.Objects[2])
	test.TapCanvas(window.Canvas(), stationPos.Add(fyne.NewPos(stationArtworkSize+20, 12)))
	if player.played.ID != american.ID || w.selectedStation.ID != american.ID {
		t.Fatal("tapping a history row's text must play its station")
	}
	if w.recent[0].Station.ID != american.ID {
		t.Fatal("playing a station from history must move it to the most recent position")
	}
	// The favorite button must receive the tap instead of the play button below it.
	italianObject := history.Objects[2].(*fyne.Container).Objects[1]
	favorite := rows.rows[italianObject].favorite
	favoritePos := app.Driver().AbsolutePositionForObject(favorite)
	test.TapCanvas(window.Canvas(), favoritePos.Add(fyne.NewPos(20, 20)))
	if !w.isFavorite(italian.ID) || w.selectedStation.ID != american.ID {
		t.Fatal("tapping a favorite must save that station without starting playback")
	}
}

// TestRecentDayUsesLocalCalendar checks day groups across DST and year boundaries.
func TestRecentDayUsesLocalCalendar(t *testing.T) {
	location, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.March, 30, 0, 30, 0, 0, location)
	for _, scenario := range []struct {
		played time.Time
		want   string
	}{
		{played: now, want: "Today"},
		{played: time.Date(2026, time.March, 29, 0, 15, 0, 0, location), want: "Yesterday"},
		{played: time.Date(2026, time.March, 28, 18, 0, 0, 0, location), want: "28 MAR"},
		{played: time.Date(2025, time.December, 31, 18, 0, 0, 0, location), want: "31 DEC 2025"},
	} {
		if got := recentDay(scenario.played.UTC(), now); got != scenario.want {
			t.Errorf("recentDay(%v) = %q, want %q", scenario.played, got, scenario.want)
		}
	}
}
