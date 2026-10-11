package ui

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TestDiscoverCountryPicker checks that suggestions overlay the results and that
// choosing, cancelling, and clearing a country retain the correct filter value.
func TestDiscoverCountryPicker(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, nil)
	w := &Window{app: app, window: window}
	s := newDiscoverSection(w)
	t.Cleanup(func() {
		if s.timer != nil {
			s.timer.Stop()
		}
		s.countryPopup.Hide()
	})
	s.countryItems = []Country{{Code: "IT", Name: "Italy"}, {Code: "US", Name: "United States"}}
	window.SetContent(s.Container)
	window.Resize(fyne.NewSize(480, 500))
	window.Show()
	if s.countryPopup.Visible() || s.count.Visible() || s.status.Visible() {
		t.Fatal("an unused search must not show suggestions or result status")
	}
	resultsPosition := s.results.Position()
	resultsSize := s.results.Size()
	window.Canvas().Focus(s.country)
	s.country.SetText("United")
	if !s.countryPopup.Visible() || len(s.countryMatch) != 1 || s.countryMatch[0].Code != "US" {
		t.Fatal("typing a country must show its matching suggestion")
	}
	if s.results.Position() != resultsPosition || s.results.Size() != resultsSize {
		t.Fatal("country suggestions must overlay the results without moving or resizing them")
	}
	s.countryList.Select(0)
	if s.countryValue.Code != "US" || s.country.Text != discoverCountryLabel(s.countryValue) || s.countryPopup.Visible() {
		t.Fatal("selecting a suggestion must save its country and close the popup")
	}
	s.timer.Stop()
	// Reopen and cancel a different search: the selected country must be restored.
	window.Canvas().Focus(s.name)
	window.Canvas().Focus(s.country)
	s.country.SetText("Italy")
	s.country.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if s.countryValue.Code != "US" || s.country.Text != discoverCountryLabel(s.countryValue) || s.countryPopup.Visible() {
		t.Fatal("Escape must restore the previous country and dismiss suggestions")
	}
	// Choosing Any country removes only that filter and restores global results.
	s.showCountryOptions("")
	s.countryList.Select(0)
	if s.countryValue.Code != "" || s.count.Visible() || !s.status.Visible() || s.status.Text != "Loading popular stations…" {
		t.Fatal("Any country must clear its filter and load global popular stations")
	}
	s.name.SetText("Jazz")
	if !s.status.Visible() {
		t.Fatal("editing the station name must show the search status")
	}
	clear := s.name.ActionItem.(*widget.Button)
	position := app.Driver().AbsolutePositionForObject(clear)
	test.TapCanvas(window.Canvas(), position.Add(fyne.NewPos(clear.Size().Width/2, clear.Size().Height/2)))
	if s.name.Text != "" || !s.status.Visible() {
		t.Fatal("clearing the station name must restore the unfiltered popular results")
	}
}

func TestDiscoverGenresUseCuratedRadioBrowserTags(t *testing.T) {
	want := []discoverGenre{
		{label: "Pop", tags: []string{"pop", "pop music", "top 40", "dance pop"}},
		{label: "Rock", tags: []string{"rock", "classic rock", "alternative rock", "pop rock"}},
		{label: "Dance", tags: []string{"dance", "disco", "dance pop"}},
		{label: "Electronic", tags: []string{"electronic", "house", "techno", "trance"}},
		{label: "80s", tags: []string{"80s"}},
		{label: "90s", tags: []string{"90s"}},
		{label: "Jazz & Blues", tags: []string{"jazz", "smooth jazz", "blues"}},
		{label: "Classical", tags: []string{"classical", "opera", "baroque"}},
		{label: "Hip-Hop & R&B", tags: []string{"hip hop", "rap", "rnb", "r&b"}},
		{label: "Classic Hits", tags: []string{"oldies", "classic hits", "retro"}},
		{label: "70s", tags: []string{"70s"}},
		{label: "2000s", tags: []string{"2000s", "00s"}},
		{label: "Metal", tags: []string{"metal", "heavy metal"}},
		{label: "Country & Folk", tags: []string{"country", "folk"}},
		{label: "Soul & Funk", tags: []string{"soul", "funk"}},
		{label: "Chill & Ambient", tags: []string{"chillout", "ambient", "lounge", "easy listening"}},
		{label: "Latin", tags: []string{"latin", "salsa", "reggaeton", "bachata"}},
		{label: "News & Talk", tags: []string{"news", "talk", "news talk", "local news"}},
		{label: "Sports", tags: []string{"sports", "sports talk"}},
	}
	if !reflect.DeepEqual(discoverGenres, want) {
		t.Fatalf("discover genres = %#v, want %#v", discoverGenres, want)
	}

	labels := discoverGenreLabels()
	if labels[0] != "Any genre" || len(labels) != len(want)+1 {
		t.Fatalf("genre labels = %#v", labels)
	}
	if tags := discoverGenreTags("2000s"); !reflect.DeepEqual(tags, []string{"2000s", "00s"}) {
		t.Fatalf("2000s tags = %#v", tags)
	}
	if tags := discoverGenreTags("Any genre"); len(tags) != 0 {
		t.Fatalf("Any genre tags = %#v, want none", tags)
	}
}
