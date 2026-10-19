package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
)

// favoritesSection shows saved stations and their empty state.
type favoritesSection struct {
	*fyne.Container
	window   *Window
	rows     *stationList
	content  *fyne.Container
	empty    fyne.CanvasObject
	add      *widget.Button
	actions  *fyne.Container
	subtitle *widget.Label
}

// newFavoritesSection builds Favorites using the same station rows as Home.
func newFavoritesSection(w *Window) *favoritesSection {
	rows := newStationList(w, false)
	content := container.NewStack()
	subtitle := widget.NewLabel("Your saved radio stations")
	add := widget.NewButtonWithIcon("", icons.Plus(), w.addCustomStation)
	add.Importance = widget.LowImportance
	section := &favoritesSection{
		window:  w,
		rows:    rows,
		content: content,
		empty:   favoritesEmptyState(w),
		add:     add,
		actions: container.NewHBox(
			container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(44), add)),
		),
		subtitle: subtitle,
	}
	section.Container = content
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
func favoritesEmptyState(w *Window) fyne.CanvasObject {
	icon := w.newThemeIcon(icons.Favorites)
	icon.Resize(fyne.NewSquareSize(36))
	title := widget.NewLabelWithStyle("No favorites yet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	title.SizeName = theme.SizeNameSubHeadingText
	detail := widget.NewLabel("Save your favorite stations\nor add your own stream.")
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	add := widget.NewButton("Add station", w.addCustomStation)
	add.Importance = widget.HighImportance
	explore := widget.NewButton("Explore stations", func() {
		if w.bottomNavigation != nil {
			w.bottomNavigation.Select(3)
		}
	})
	explore.Importance = widget.LowImportance
	buttonSize := fyne.NewSize(220, 48)
	return container.NewVScroll(container.NewCenter(container.NewVBox(
		container.NewCenter(icon),
		container.NewCenter(title),
		container.NewCenter(detail),
		container.NewGridWrap(buttonSize, add),
		container.NewGridWrap(buttonSize, explore),
	)))
}
