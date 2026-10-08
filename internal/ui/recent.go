package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/storage"
)

const recentStationLimit = 20

// recentSection shows recently played stations and an empty state.
type recentSection struct {
	*fyne.Container
	window  *Window
	rows    *stationList
	content *fyne.Container
	empty   fyne.CanvasObject
}

// newRecentSection creates Recent with the shared station list in recent mode.
func newRecentSection(w *Window) *recentSection {
	s := &recentSection{
		window:  w,
		rows:    newStationList(w, true),
		content: container.NewStack(),
		empty:   recentEmptyState(),
	}
	s.Container = s.content
	s.refresh()
	return s
}

// refresh updates the shared list or its empty state after history changes.
func (s *recentSection) refresh() {
	s.rows.setRecent(s.window.recent)
	if len(s.window.recent) == 0 {
		s.content.Objects = []fyne.CanvasObject{s.empty}
	} else {
		s.content.Objects = []fyne.CanvasObject{s.rows.list}
	}
	s.content.Refresh()
}

// recentEmptyState explains that played stations will appear in this section.
func recentEmptyState() fyne.CanvasObject {
	icon := widget.NewIcon(theme.HistoryIcon())
	icon.Resize(fyne.NewSquareSize(42))
	title := widget.NewLabelWithStyle("Nothing played yet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel("Stations you play will appear here.")
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewVBox(icon, title, detail))
}

// recentTimestamp formats the local playback date and time for a row.
func recentTimestamp(playedAt time.Time) string {
	now := time.Now()
	played := playedAt.In(now.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(played.Year(), played.Month(), played.Day(), 0, 0, 0, 0, now.Location())
	date := played.Format("2 Jan")
	switch {
	case day.Equal(today):
		date = "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		date = "Yesterday"
	}
	return date + ", " + played.Format("15:04")
}

// recordRecent moves a station to the top and keeps the history bounded.
func recordRecent(recent []storage.RecentStation, station storage.RecentStation) []storage.RecentStation {
	updated := make([]storage.RecentStation, 0, min(len(recent)+1, recentStationLimit))
	updated = append(updated, station)
	for _, item := range recent {
		if item.Station.ID == station.Station.ID {
			continue
		}
		if len(updated) == recentStationLimit {
			break
		}
		updated = append(updated, item)
	}
	return updated
}
