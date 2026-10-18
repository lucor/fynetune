package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
)

func TestFavoritesOffersCustomStationForm(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, nil)
	w := &Window{app: app, window: window, store: storage.New(app)}
	favorites := newFavoritesSection(w)
	w.favorites = favorites
	w.appContent = favorites.Container
	window.SetContent(favorites.actions)
	window.Show()

	if favorites.empty == nil || favorites.add == nil {
		t.Fatal("empty Favorites must provide add-station actions")
	}
	if got := favorites.add.Icon.Name(); got != "lucide-light-plus.png" {
		t.Fatalf("Favorites add icon = %q, want the Lucide plus asset", got)
	}
	if got := favorites.actions.MinSize().Height; got != 44 {
		t.Fatalf("Favorites add action height = %v, want the 44-unit target used by station row actions", got)
	}
	renderer := test.WidgetRenderer(favorites.add)
	background := renderer.Objects()[0].(*canvas.Rectangle)
	beforeR, beforeG, beforeB, beforeA := background.FillColor.RGBA()
	position := app.Driver().AbsolutePositionForObject(favorites.add).Add(fyne.NewPos(favorites.add.Size().Width/2, favorites.add.Size().Height/2))
	test.MoveMouse(window.Canvas(), position)
	afterR, afterG, afterB, afterA := background.FillColor.RGBA()
	if beforeR == afterR && beforeG == afterG && beforeB == afterB && beforeA == afterA {
		t.Fatal("hovering the Favorites add action should show button feedback")
	}
	test.MoveMouse(window.Canvas(), fyne.NewPos(-1, -1))
	afterR, afterG, afterB, afterA = background.FillColor.RGBA()
	if beforeR != afterR || beforeG != afterG || beforeB != afterB || beforeA != afterA {
		t.Fatal("leaving the Favorites add action should clear its hover feedback")
	}
	test.Tap(favorites.add)
	if window.Content() == w.appContent || window.Canvas().Overlays().Top() != nil {
		t.Fatal("the Add station action must open a full-window form")
	}
	w.editCustomStation(radio.Station{
		ID: "custom-1", Source: radio.StationSourceCustom, Name: "Local", URL: "https://example.com/live",
	})
	if window.Content() == w.appContent || window.Canvas().Overlays().Top() != nil {
		t.Fatal("editing a custom station must open the shared full-window form")
	}
}

func TestCustomStationFavoritesRowOmitsFavoriteHeart(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, nil)
	w := &Window{app: app, window: window, store: storage.New(app), settings: storage.Settings{
		Stations: []radio.Station{{ID: "custom-1", Source: radio.StationSourceCustom, Name: "Local", URL: "https://example.com/live"}},
	}}
	favorites := newFavoritesSection(w)
	w.favorites = favorites
	window.SetContent(favorites.Container)
	window.Show()

	var row *stationListRow
	var item fyne.CanvasObject
	for object, visibleRow := range favorites.rows.rows {
		item = object
		row = visibleRow
		if visibleRow.station.ID == "custom-1" {
			break
		}
	}
	if row == nil || item == nil {
		t.Fatal("custom station row was not rendered")
	}
	favorites.rows.updateRow(0, item)
	if row.favorite.Visible() {
		t.Fatalf("custom station rows must not display a favorite heart: station=%+v", row.station)
	}
	if !row.manage.Visible() {
		t.Fatal("custom station rows must display their management menu")
	}
}

func TestCustomStationManagementMenuAppearsInRecent(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, nil)
	station := radio.Station{ID: "custom-1", Source: radio.StationSourceCustom, Name: "Local", URL: "https://example.com/live"}
	w := &Window{
		app: app, window: window, store: storage.New(app),
		recent: []storage.RecentStation{{Station: station}},
	}
	recent := newRecentSection(w)
	w.recentSection = recent
	window.SetContent(recent.Container)
	window.Show()

	var row *stationListRow
	for _, visibleRow := range recent.rows.rows {
		row = visibleRow
		break
	}
	if row == nil || !row.manage.Visible() {
		t.Fatal("custom station rows in Recent must display their management menu")
	}
	test.Tap(row.manage)
	if window.Canvas().Overlays().Top() == nil {
		t.Fatal("the custom station overflow button must open its menu")
	}
}

func TestDeletingCustomStationCleansRecentAndPlaybackSelection(t *testing.T) {
	app := test.NewTempApp(t)
	window := test.NewTempWindow(t, nil)
	store := storage.New(app)
	station := radio.Station{ID: "custom-1", Source: radio.StationSourceCustom, Name: "Local", URL: "https://example.com/live"}
	other := radio.Station{ID: "directory-1", Source: radio.StationSourceRadioBrowser, Name: "Directory", URL: "https://example.com/radio"}
	settings := storage.Settings{Stations: []radio.Station{station, other}, Selected: station.ID, Volume: 0.5}
	if err := store.SaveStations(settings.Stations); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	recent := []storage.RecentStation{{Station: station}, {Station: other}}
	if err := store.SaveRecent(recent); err != nil {
		t.Fatal(err)
	}
	player := &trayTestPlayer{events: make(chan radio.Event), state: radio.StatePlaying}
	w := &Window{
		app: app, window: window, store: store, settings: settings,
		recent: recent, selectedStation: station, radioPlayer: player,
	}
	if err := w.deleteCustomStation(station); err != nil {
		t.Fatal(err)
	}
	if player.state != radio.StateStopped || w.selectedStation.ID != "" || w.settings.Selected != "" {
		t.Fatal("deleting the selected station must stop playback and clear its selection")
	}
	if len(w.settings.Stations) != 1 || w.settings.Stations[0].ID != other.ID {
		t.Fatalf("favorites after deletion = %+v", w.settings.Stations)
	}
	if len(w.recent) != 1 || w.recent[0].Station.ID != other.ID {
		t.Fatalf("recent stations after deletion = %+v", w.recent)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Selected != "" || len(loaded.Stations) != 1 || loaded.Stations[0].ID != other.ID {
		t.Fatalf("persisted settings after deletion = %+v", loaded)
	}
	loadedRecent, err := store.LoadRecent()
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedRecent) != 1 || loadedRecent[0].Station.ID != other.ID {
		t.Fatalf("persisted recent stations after deletion = %+v", loadedRecent)
	}
}

func TestSavingCustomStationRefreshesFavorites(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, nil)
	w := &Window{app: app, window: window, store: storage.New(app)}
	favorites := newFavoritesSection(w)
	w.favorites = favorites
	window.SetContent(favorites.Container)
	window.Show()

	station, err := radio.NewCustomStation("Local", "https://example.com/live", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	w.saveCustomStation(station, false)
	if len(w.settings.Stations) != 1 || w.settings.Stations[0].ID != station.ID {
		t.Fatalf("saved favorites = %+v, want the custom station", w.settings.Stations)
	}
	if len(favorites.rows.entries) != 1 || favorites.rows.entries[0].station.ID != station.ID {
		t.Fatalf("Favorites entries = %+v, want the saved custom station", favorites.rows.entries)
	}
}

func TestEditingCustomStationThroughFormSavesChanges(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, nil)
	store := storage.New(app)
	existing := radio.Station{ID: "custom-1", Source: radio.StationSourceCustom, Name: "Local", URL: "https://example.com/live"}
	recent := []storage.RecentStation{{Station: existing}}
	if err := store.SaveRecent(recent); err != nil {
		t.Fatal(err)
	}
	w := &Window{
		app: app, window: window, store: store,
		settings:   storage.Settings{Stations: []radio.Station{existing}},
		appContent: widget.NewLabel("App"),
		recent:     recent, selectedStation: existing,
	}
	w.editCustomStation(existing)
	window.Show()

	var form *widget.Form
	var save *widget.Button
	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		switch object := object.(type) {
		case *widget.Form:
			form = object
		case *widget.Button:
			if object.Text == "Save" {
				save = object
			}
		case *fyne.Container:
			for _, child := range object.Objects {
				walk(child)
			}
		case *container.Scroll:
			walk(object.Content)
		}
	}
	walk(window.Content())
	if form == nil || save == nil || len(form.Items) != 3 {
		t.Fatalf("edit form controls not found: form=%v save=%v", form != nil, save != nil)
	}
	form.Items[0].Widget.(*widget.Entry).SetText("Updated name")
	form.Items[1].Widget.(*widget.Entry).SetText("https://example.org/new-stream")
	form.Items[2].Widget.(*widget.Entry).SetText("https://example.org")
	test.Tap(save)

	if window.Content() != w.appContent {
		t.Fatal("saving a valid edit should return to the app")
	}
	if len(w.settings.Stations) != 1 {
		t.Fatalf("saved stations = %+v, want one station", w.settings.Stations)
	}
	updated := w.settings.Stations[0]
	if updated.ID != existing.ID || updated.Name != "Updated name" || updated.URL != "https://example.org/new-stream" || updated.Homepage != "https://example.org" {
		t.Fatalf("updated station = %+v", updated)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Stations) != 1 || loaded.Stations[0].ID != updated.ID || loaded.Stations[0].Name != updated.Name || loaded.Stations[0].URL != updated.URL || loaded.Stations[0].Homepage != updated.Homepage {
		t.Fatalf("persisted station = %+v, want %+v", loaded.Stations, updated)
	}
	loadedRecent, err := store.LoadRecent()
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedRecent) != 1 || loadedRecent[0].Station.Name != updated.Name || loadedRecent[0].Station.URL != updated.URL {
		t.Fatalf("persisted Recent station = %+v, want edited station %+v", loadedRecent, updated)
	}
	if w.selectedStation.ID != updated.ID || w.selectedStation.Name != updated.Name {
		t.Fatalf("selected station after edit = %+v, want %+v", w.selectedStation, updated)
	}
}
