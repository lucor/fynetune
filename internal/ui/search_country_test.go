package ui

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSearchCountryPickerFiltersSelectsAndRestores(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	w := &Window{
		countryOptions: []string{"Any country", "Italy", "Japan"},
		page:           pageSearch,
	}
	win := app.NewWindow("country search test")
	defer win.Close()
	win.SetContent(w.searchPage())
	win.Resize(fyne.NewSize(480, 640))
	win.Show()

	win.Canvas().Focus(w.searchCountry)
	test.Type(w.searchCountry, "Japan")
	if got, want := w.searchCountryMatches, []string{"Japan"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("country matches = %#v, want %#v", got, want)
	}
	w.searchCountryList.OnSelected(widget.ListItemID(0))
	if w.searchCountryValue != "Japan" {
		t.Fatalf("selected country = %q, want Japan", w.searchCountryValue)
	}
	if w.searchCountryOptionsVisible {
		t.Fatal("country options remained open after selection")
	}
	if w.searchTimer != nil {
		w.searchTimer.Stop()
	}

	win.Canvas().Focus(nil)
	win.Canvas().Focus(w.searchCountry)
	test.Type(w.searchCountry, "Italy")
	w.searchCountry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if w.searchCountry.Text != "Japan" || w.searchCountryValue != "Japan" {
		t.Fatalf("Escape did not restore selection: text=%q value=%q", w.searchCountry.Text, w.searchCountryValue)
	}
	if w.searchCountryOptionsVisible {
		t.Fatal("country options remained open after Escape")
	}
}
