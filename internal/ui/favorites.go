package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// favoritesSection shows saved stations and their empty state.
type favoritesSection struct {
	*fyne.Container
	window  *Window
	rows    *stationList
	content *fyne.Container
	empty   fyne.CanvasObject
}

// newFavoritesSection builds Favorites using the same station rows as Home.
func newFavoritesSection(w *Window) *favoritesSection {
	rows := newStationList(w, false)
	content := container.NewStack()
	section := &favoritesSection{
		window:  w,
		rows:    rows,
		content: content,
		empty:   favoritesEmptyState(),
	}
	section.Container = container.NewBorder(
		widget.NewLabel("Your saved radio stations"), nil, nil, nil, content,
	)
	section.refresh()
	return section
}

// refresh synchronizes the saved stations and selects the list or empty state.
func (s *favoritesSection) refresh() {
	stations := s.window.settings.Stations
	s.rows.setStations(stations)
	if len(stations) == 0 {
		s.content.Objects = []fyne.CanvasObject{s.empty}
	} else {
		s.content.Objects = []fyne.CanvasObject{s.rows.list}
	}
	s.content.Refresh()
}

// favoritesEmptyState explains how to add the first saved station.
func favoritesEmptyState() fyne.CanvasObject {
	icon := widget.NewIcon(homeFavoriteOutline)
	icon.Resize(fyne.NewSquareSize(42))
	title := widget.NewLabelWithStyle("No favorites yet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel("Save a station from Radio to keep it here.")
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewVBox(icon, title, detail))
}
