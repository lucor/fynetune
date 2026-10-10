package ui

import (
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
)

const recentStationLimit = 20

// recentSection shows recently played stations and an empty state.
type recentSection struct {
	*fyne.Container
	window   *Window
	rows     *stationList
	content  *fyne.Container
	empty    fyne.CanvasObject
	subtitle *widget.Label
	history  *fyne.Container
	scroll   *container.Scroll
}

// newRecentSection creates Recent with the shared station list in recent mode.
func newRecentSection(w *Window) *recentSection {
	s := &recentSection{
		window:   w,
		rows:     newStationList(w, true),
		content:  container.NewStack(),
		empty:    recentEmptyState(w),
		subtitle: widget.NewLabel("Recently played stations"),
	}
	s.history = container.NewVBox()
	s.scroll = container.NewVScroll(s.history)
	s.Container = s.content
	s.refresh()
	return s
}

// refresh builds the bounded history from native labels and buttons. Day headings
// sit outside the buttons, so they have no tap, focus, selection, or hover behavior.
func (s *recentSection) refresh() {
	s.rows.setRecent(s.window.recent)
	s.history.RemoveAll()
	now := time.Now()
	lastDay := ""
	for i, entry := range s.rows.entries {
		day := recentDay(entry.playedAt, now)
		if day != lastDay {
			heading := widget.NewLabelWithStyle(day, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
			heading.SizeName = theme.SizeNameCaptionText
			s.history.Add(heading)
			lastDay = day
		}
		row := s.rows.newRow()
		s.rows.updateRow(i, row)
		play := widget.NewButton("", func() {
			s.rows.setPlayerArtwork(entry.station)
			s.window.selectStation(entry.station)
		})
		play.Importance = widget.LowImportance
		// The native button supplies interaction below the row's labels and artwork;
		// the favorite button above it handles its own taps.
		s.history.Add(container.NewStack(play, row))
	}
	if len(s.window.recent) == 0 {
		s.content.Objects = []fyne.CanvasObject{s.empty}
	} else {
		s.content.Objects = []fyne.CanvasObject{s.scroll}
	}
	s.scroll.Refresh()
	s.content.Refresh()
}

// recentEmptyState explains that played stations will appear in this section.
func recentEmptyState(w *Window) fyne.CanvasObject {
	icon := w.newThemeIcon(icons.Recent)
	icon.Resize(fyne.NewSquareSize(42))
	title := widget.NewLabelWithStyle("Nothing played yet", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel("Stations you play will appear here.")
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewVBox(icon, title, detail))
}

// recentDay labels a history group using local calendar days, including DST changes.
func recentDay(playedAt, now time.Time) string {
	played := playedAt.In(now.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(played.Year(), played.Month(), played.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case day.Equal(today):
		return "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	}
	if played.Year() != now.Year() {
		return strings.ToUpper(played.Format("2 Jan 2006"))
	}
	return strings.ToUpper(played.Format("2 Jan"))
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
