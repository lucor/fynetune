package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
)

type stationListEntry struct {
	station radio.Station
	details stationRowDetails
	day     string
	menu    bool
}

type stationListView struct {
	entries       []stationListEntry
	list          *widget.List
	rowHeight     float32
	dayHeight     float32
	highlightedID string
}

type stationRowView struct {
	widget.BaseWidget
	entry    stationListEntry
	text     *widget.RichText
	artwork  *fyne.Container
	image    *canvas.Image
	favorite *widget.Button
	area     *stationTapArea
	day      *widget.Label
	content  fyne.CanvasObject
}

func (w *Window) newStationRowView() *stationRowView {
	v := &stationRowView{text: stationRowDetailView(stationRowDetails{metadata: "Station details", codec: "MP3 · 128 kbps"}), day: widget.NewLabelWithStyle("Today", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})}
	v.ExtendBaseWidget(v)
	v.text.Segments = append([]widget.RichTextSegment{&widget.TextSegment{Text: "Station\n", Style: widget.RichTextStyleStrong}}, v.text.Segments...)
	v.day.Hide()
	v.image = newStationArtworkImage()
	v.artwork = stationArtworkFrame(v.image)
	v.favorite = iconButton(favoriteRowOutline, func() { w.toggleFavorite(v.entry.station) })
	favorite := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(favoriteTouchSize), v.favorite))
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(stationArtworkTextGap, 0))
	icon := container.New(layout.NewCustomPaddedHBoxLayout(0), container.NewCenter(v.artwork), gap)
	content := container.NewBorder(nil, nil, icon, nil, v.text)
	v.area = newStationTapArea(content, func() { w.playStation(v.entry.station) }, func() {
		if v.entry.menu {
			w.stationMenu(v.entry.station)
		}
	})
	row := container.NewPadded(container.NewBorder(nil, nil, nil, favorite, v.area))
	v.content = container.NewVBox(v.day, row)
	return v
}

func (v *stationRowView) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(v.content)
}

func (w *Window) stationList(key page, entries []stationListEntry) *widget.List {
	if w.stationLists == nil {
		w.stationLists = make(map[page]*stationListView)
	}
	v := w.stationLists[key]
	if v == nil {
		v = &stationListView{}
		v.list = widget.NewList(
			func() int { return len(v.entries) },
			func() fyne.CanvasObject {
				cell := w.newStationRowView()
				v.rowHeight = cell.MinSize().Height
				v.dayHeight = cell.day.MinSize().Height + theme.Size(theme.SizeNamePadding)
				return cell
			},
			func(id widget.ListItemID, object fyne.CanvasObject) {
				if int(id) >= len(v.entries) {
					return
				}
				cell := object.(*stationRowView)
				w.updateStationRow(cell, v.entries[id])
			},
		)
		v.list.HideSeparators = true
		w.stationLists[key] = v
	}
	clear(v.entries)
	v.entries = append(v.entries[:0], entries...)
	// The list measures its template before receiving station data. Keep room
	// for the title and both detail lines, and add a date heading only where used.
	v.list.MinSize()
	for id, entry := range v.entries {
		height := v.rowHeight
		if entry.day != "" {
			height += v.dayHeight
		}
		v.list.SetItemHeight(id, height)
	}
	v.list.Refresh()
	v.highlightedID = w.selected.ID
	return v.list
}

func (w *Window) updateStationRow(cell *stationRowView, entry stationListEntry) {
	previous := cell.entry
	cell.entry = entry
	if entry.day == "" {
		cell.day.Hide()
	} else {
		cell.day.SetText(entry.day)
		cell.day.Show()
	}
	title := cell.text.Segments[0].(*widget.TextSegment)
	details := cell.text.Segments[1].(*widget.TextSegment)
	text := stationDetailText(entry.details)
	if title.Text != entry.station.Name+"\n" || details.Text != text {
		title.Text = entry.station.Name + "\n"
		details.Text = text
		cell.text.Refresh()
	}
	if previous.station.ID != entry.station.ID || previous.station.Favicon != entry.station.Favicon || previous.station.Homepage != entry.station.Homepage {
		w.setStationImage(cell.image, entry.station, func() bool {
			return cell.entry.station.ID == entry.station.ID && cell.entry.station.Favicon == entry.station.Favicon && cell.entry.station.Homepage == entry.station.Homepage
		})
	}
	favorite := favoriteRowOutline
	if w.isFavorite(entry.station.ID) {
		favorite = favoriteRowFilled
	}
	cell.favorite.SetIcon(favorite)
	cell.area.stationID = entry.station.ID
	cell.area.SetHighlighted(w.stationIsActive(entry.station.ID))
}

func (w *Window) refreshStationLists() {
	key := w.page
	view := w.stationLists[key]
	if view == nil {
		return
	}
	if view.highlightedID == w.selected.ID {
		return
	}
	view.highlightedID = w.selected.ID
	view.list.Refresh()
}
