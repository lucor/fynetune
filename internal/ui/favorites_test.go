package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

func TestEmptyFavoritesUsesHeartIcon(t *testing.T) {
	w := &Window{}
	page := w.stationPage("Favorites", "Your saved radio stations", nil)

	center, ok := page.Objects[1].(*fyne.Container)
	if !ok || len(center.Objects) != 1 {
		t.Fatal("empty favorites state is missing")
	}
	content, ok := center.Objects[0].(*fyne.Container)
	if !ok || len(content.Objects) == 0 {
		t.Fatal("empty favorites content is missing")
	}
	icon, ok := content.Objects[0].(*widget.Icon)
	if !ok {
		t.Fatalf("empty favorites icon has type %T, want *widget.Icon", content.Objects[0])
	}
	if got, want := icon.Resource.Name(), "favorite-outline.png"; got != want {
		t.Fatalf("empty favorites icon = %q, want %q", got, want)
	}
}
