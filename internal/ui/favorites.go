package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
	"go.lucor.dev/fynetune/internal/ui/widgets"
)

// favoritesSection shows saved stations and their empty state.
type favoritesSection struct {
	*fyne.Container
	window   *Window
	rows     *stationList
	content  *fyne.Container
	empty    fyne.CanvasObject
	saved    *widgets.Badge
	subtitle *widget.Label
}

// newFavoritesSection builds Favorites using the same station rows as Home.
func newFavoritesSection(w *Window) *favoritesSection {
	rows := newStationList(w, false)
	content := container.NewStack()
	saved := widgets.NewBadge(
		"",
		theme.Color(theme.ColorNameSelection),
		theme.Color(stationMetadataColorName),
	)
	subtitle := widget.NewLabel("Your saved radio stations")
	section := &favoritesSection{
		window:   w,
		rows:     rows,
		content:  content,
		empty:    favoritesEmptyState(w),
		saved:    saved,
		subtitle: subtitle,
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
	s.saved.SetText(fmt.Sprintf("%d saved", len(stations)))
	s.rows.setStations(stations)
	if len(stations) == 0 {
		s.content.Objects = []fyne.CanvasObject{s.empty}
	} else {
		s.content.Objects = []fyne.CanvasObject{s.rows.list}
	}
	s.content.Refresh()
}

// refreshTheme applies the active theme colors to the saved-count badge.
func (s *favoritesSection) refreshTheme() {
	s.saved.SetColors(
		theme.Color(theme.ColorNameSelection),
		theme.Color(stationMetadataColorName),
	)
}

// favoritesEmptyState explains how to add the first saved station.
func favoritesEmptyState(w *Window) fyne.CanvasObject {
	icon := w.newThemeIcon(icons.Favorites)
	icon.Resize(fyne.NewSquareSize(42))
	title := widget.NewLabelWithStyle("No favorites yet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel("Save a station from Radio to keep it here.")
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewVBox(icon, title, detail))
}
