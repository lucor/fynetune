package ui

import (
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"go.lucor.dev/fynetune/internal/directory/radiobrowser"
)

func TestRegionOptionsPreselectsLocaleCountryWithoutSuggestionLabel(t *testing.T) {
	w := &Window{
		regionSuggestionCode: "IT",
		countries: []radiobrowser.Country{
			{Name: "Italy", Code: "IT"},
			{Name: "Japan", Code: "JP"},
		},
	}

	want := []string{"Any country", "Italy", "Japan"}
	if got := w.regionOptions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("region options = %#v, want %#v", got, want)
	}
	if got := w.initialRegionOption(w.regionOptions()); got != "Italy" {
		t.Fatalf("initial region = %q, want Italy", got)
	}
}

func TestOnboardingIconHasFixedDisplaySize(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := &Window{regionSuggestionCode: "IT"}
	page := w.regionWelcomePage()
	column := page.Objects[0].(*fyne.Container)
	logoCenter := column.Objects[1].(*fyne.Container)
	logoSlot := logoCenter.Objects[0].(*fyne.Container)
	if got, want := logoSlot.MinSize(), fyne.NewSize(80, 80); got != want {
		t.Fatalf("logo slot size = %v, want %v", got, want)
	}

	column.Resize(column.MinSize())
	logo := logoSlot.Objects[0].(*canvas.Image)
	if got, want := logo.Size(), fyne.NewSize(80, 80); got != want {
		t.Fatalf("logo image size = %v, want %v", got, want)
	}
	if column.Objects[6] != w.regionContinue {
		t.Fatal("continue button is not in the expected position")
	}
	note, ok := column.Objects[7].(*widget.RichText)
	if !ok || !strings.Contains(note.Segments[0].Textual(), "regional settings") {
		t.Fatal("privacy note is missing after the continue button")
	}
	if note.Segments[0].(*widget.TextSegment).Style.SizeName != theme.SizeNameText {
		t.Fatal("privacy note should use standard text size for readability")
	}
	if note.Wrapping != fyne.TextWrapWord {
		t.Fatal("privacy note should wrap at word boundaries")
	}
}

func TestRegionWelcomePageGolden(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := &Window{
		regionSuggestionCode: "IT",
		countries:            []radiobrowser.Country{{Name: "Italy", Code: "IT"}},
	}
	page := w.regionWelcomePage()
	page.Resize(fyne.NewSize(480, 560))
	test.AssertObjectRendersToImage(t, "region_welcome.png", page)
}

func TestFilterRegionOptions(t *testing.T) {
	options := []string{"Any country", "France", "Italy", "United Kingdom"}
	tests := []struct {
		name, query string
		want        []string
	}{
		{name: "empty shows all", want: options},
		{name: "case insensitive", query: "it", want: []string{"Italy", "United Kingdom"}},
		{name: "trim query", query: "  france ", want: []string{"France"}},
		{name: "no results", query: "japan", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterRegionOptions(options, tt.query); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("filtered options = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRegionPickerFocusTypingEscapeAndSelection(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := &Window{
		regionSuggestionCode: "IT",
		countries: []radiobrowser.Country{
			{Name: "Italy", Code: "IT"},
			{Name: "Japan", Code: "JP"},
		},
	}
	content := w.regionWelcomePage()
	win := app.NewWindow("region picker test")
	defer win.Close()
	win.SetContent(content)
	win.Resize(fyne.NewSize(480, 640))

	win.Canvas().Focus(w.regionSelect)
	if focused := win.Canvas().Focused(); focused != w.regionSelect {
		t.Fatalf("focused widget = %T, want searchable region entry", focused)
	}
	if w.regionSelect.Text != "" {
		t.Fatalf("focus text = %q, want empty", w.regionSelect.Text)
	}
	if !w.regionOptionsVisible {
		t.Fatal("country list did not open on focus")
	}

	test.Type(w.regionSelect, "Japan")
	if w.regionSelect.Text != "Japan" {
		t.Fatalf("typed text = %q, want Japan", w.regionSelect.Text)
	}
	if !reflect.DeepEqual(w.regionMatches, []string{"Japan"}) {
		t.Fatalf("filtered countries = %#v, want [Japan]", w.regionMatches)
	}

	w.regionSelect.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if w.regionSelect.Text != "Italy" {
		t.Fatalf("Escape text = %q, want previous choice Italy", w.regionSelect.Text)
	}
	if w.regionOptionsVisible {
		t.Fatal("country list remained open after Escape")
	}

	w.showRegionOptions("")
	for id, country := range w.regionMatches {
		if country == "Japan" {
			w.regionList.OnSelected(widget.ListItemID(id))
			if w.regionSelect.Text != "Japan" {
				t.Fatalf("selected text = %q, want Japan", w.regionSelect.Text)
			}
			if w.regionOptionsVisible {
				t.Fatal("country list remained open after selection")
			}
			return
		}
	}
	t.Fatal("Japan was not available in the country list")
}

func TestRegionPickerTapSelectsCountry(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := &Window{
		regionSuggestionCode: "IT",
		countries: []radiobrowser.Country{
			{Name: "Italy", Code: "IT"},
			{Name: "Japan", Code: "JP"},
		},
	}
	win := app.NewWindow("region picker tap test")
	defer win.Close()
	win.SetContent(w.regionWelcomePage())
	win.Resize(fyne.NewSize(480, 640))
	win.Show()
	w.showRegionOptions("")
	win.Canvas().Refresh(w.regionListContainer)

	position := app.Driver().AbsolutePositionForObject(w.regionList)
	rowHeight := w.regionList.MinSize().Height
	if w.regionList.Size().Width == 0 || w.regionList.Size().Height == 0 {
		t.Fatalf("country list has no size after opening: %v", w.regionList.Size())
	}
	test.TapCanvas(win.Canvas(), position.Add(fyne.NewPos(20, rowHeight*2+rowHeight/2)))

	if w.regionSelect.Text != "Japan" {
		t.Fatalf("tapped selection = %q, want Japan", w.regionSelect.Text)
	}
	if w.regionOptionsVisible {
		t.Fatal("country list remained open after tap")
	}
}

func TestRegionPickerTabAndSpaceSelectCountry(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := &Window{
		regionSuggestionCode: "IT",
		countries: []radiobrowser.Country{
			{Name: "Italy", Code: "IT"},
			{Name: "Japan", Code: "JP"},
		},
	}
	win := app.NewWindow("region picker keyboard test")
	defer win.Close()
	win.SetContent(w.regionWelcomePage())
	win.Resize(fyne.NewSize(480, 640))
	win.Show()
	win.Canvas().Focus(w.regionSelect)

	for i := 0; i < 4 && win.Canvas().Focused() != w.regionList; i++ {
		win.Canvas().FocusNext()
	}
	if win.Canvas().Focused() != w.regionList {
		t.Fatalf("Tab focus reached %T, want country list", win.Canvas().Focused())
	}

	w.regionList.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	w.regionList.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDown})
	w.regionList.TypedKey(&fyne.KeyEvent{Name: fyne.KeySpace})
	if w.regionSelect.Text != "Japan" {
		t.Fatalf("keyboard selection = %q, want Japan", w.regionSelect.Text)
	}
	if w.regionOptionsVisible {
		t.Fatal("country list remained open after keyboard selection")
	}
}

func TestDiscoveryCountryForSelection(t *testing.T) {
	countries := []radiobrowser.Country{{Name: "Italy", Code: "IT"}, {Name: "Japan", Code: "JP"}}
	tests := []struct {
		name, selected, want string
	}{
		{name: "preselected locale country", selected: "Italy", want: "@IT"},
		{name: "another country", selected: "Japan", want: "@JP"},
		{name: "any country", selected: "Any country", want: "*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := discoveryCountryForSelection(tt.selected, "IT", countries)
			if got != tt.want {
				t.Fatalf("discovery country = %q, want %q", got, tt.want)
			}
		})
	}
}
