package ui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
)

type stationListTestPlayer struct {
	state   radio.PlayerState
	station radio.Station
	events  chan radio.Event
}

func (p *stationListTestPlayer) Play(station radio.Station) {
	p.station, p.state = station, radio.StatePlaying
}
func (p *stationListTestPlayer) Stop()                      { p.state = radio.StateStopped }
func (p *stationListTestPlayer) State() radio.PlayerState   { return p.state }
func (p *stationListTestPlayer) Events() <-chan radio.Event { return p.events }
func (*stationListTestPlayer) SetVolume(float64)            {}
func (*stationListTestPlayer) SetReconnect(bool)            {}
func (*stationListTestPlayer) Close() error                 { return nil }

func TestStationListsKeepStableWidgetsWhenSwitchingPages(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(brandTheme{Theme: a.Settings().Theme()})
	player := &stationListTestPlayer{events: make(chan radio.Event)}
	w := New(a, player)
	defer w.Close()
	w.regionSetupNeeded = false
	stations := []radio.Station{{ID: "one", Name: "Station One"}, {ID: "two", Name: "Station Two"}}
	w.settings.Stations = stations
	w.page = pageFavorites
	w.refreshPage()
	w.win.Canvas().Capture()
	favorites := w.stationLists[pageFavorites]
	if favorites == nil {
		t.Fatal("Fyne List did not create reusable favorite rows")
	}
	favoritePage := w.pages[pageFavorites].root
	for i := range 20 {
		w.playStation(stations[i%len(stations)])
	}
	if w.stationLists[pageFavorites] != favorites {
		t.Fatal("playing stations replaced the favorite Fyne List")
	}
	w.page = pageDiscover
	w.directoryStations = stations
	w.refreshPage()
	w.refreshDiscoveryRows()
	w.win.Canvas().Capture()
	discovery := w.stationLists[pageDiscover]
	if discovery == nil || discovery == favorites {
		t.Fatal("discovery did not retain its own Fyne List")
	}
	w.page = pageSearch
	w.query = "Station"
	w.refreshPage()
	w.refreshDiscoveryRows()
	w.win.Canvas().Capture()
	if w.stationLists[pageSearch].list == discovery.list {
		t.Fatal("search and discovery attach the same list to different page trees")
	}
	w.page = pageFavorites
	w.refreshPage()
	w.win.Canvas().Capture()
	if w.stationLists[pageFavorites] != favorites {
		t.Fatal("returning to favorites replaced its Fyne List")
	}
	if w.pages[pageFavorites].root != favoritePage {
		t.Fatal("returning to favorites rebuilt the section")
	}
}

func TestRecentRowsKeepDateHeadingsAndUpdatePlayingOrder(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	p := &stationListTestPlayer{events: make(chan radio.Event)}
	w := New(a, p)
	defer w.Close()
	w.cacheStore = nil
	w.regionSetupNeeded = false
	w.recent = []recentStation{{Station: radio.Station{ID: "one", Name: "One"}, PlayedAt: time.Now()}, {Station: radio.Station{ID: "two", Name: "Two"}, PlayedAt: time.Now()}}
	w.page = pageRecent
	w.refreshPage()
	w.win.Canvas().Capture()
	page := w.pages[pageRecent].root
	rows := visibleStationRows(w.stationLists[pageRecent].list)
	if len(rows) != 2 {
		t.Fatalf("recent rows = %d, want 2", len(rows))
	}
	if !rows[0].day.Visible() || rows[1].day.Visible() {
		t.Fatal("recent date heading is missing or repeated")
	}
	for _, row := range rows {
		if row.content.MinSize().Height > row.Size().Height {
			t.Fatal("recent date heading or station text is clipped")
		}
	}
	rows[1].area.Tapped(nil)
	w.win.Canvas().Capture()
	if w.pages[pageRecent].root != page {
		t.Fatal("playing rebuilt the recent section")
	}
	rows = visibleStationRows(w.stationLists[pageRecent].list)
	if rows[0].entry.station.ID != "two" || rows[0].text.Segments[0].Textual() != "Two\n" {
		t.Fatal("recent did not display the selected station at the top")
	}
}

func visibleStationRows(list *widget.List) []*stationRowView {
	var rows []*stationRowView
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		switch object := object.(type) {
		case *stationRowView:
			rows = append(rows, object)
		case *fyne.Container:
			for _, child := range object.Objects {
				visit(child)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(object).Objects() {
				visit(child)
			}
		}
	}
	visit(list)
	return rows
}

func TestStationRowsRefreshDataAndCallbacksAfterReuse(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	p := &stationListTestPlayer{events: make(chan radio.Event)}
	w := New(a, p)
	defer w.Close()
	w.cacheStore = nil
	w.regionSetupNeeded = false
	w.settings.Stations = []radio.Station{{ID: "one", Name: "Station One", Country: "Italy", Codec: "MP3", Bitrate: 128}, {ID: "two", Name: "Station Two"}}
	w.page = pageFavorites
	w.refreshPage()
	w.win.Canvas().Capture()
	list := w.stationLists[pageFavorites].list
	rows := visibleStationRows(list)
	if len(rows) != 2 {
		t.Fatalf("visible rows = %d, want 2", len(rows))
	}
	first := rows[0]
	text, artwork := first.text, first.artwork.Objects[0]
	w.refreshPlayingRows()
	w.win.Canvas().Capture()
	if first.text != text || first.artwork.Objects[0] != artwork {
		t.Fatal("playback refresh replaced unchanged text or artwork")
	}
	stations := []radio.Station{w.settings.Stations[1], w.settings.Stations[0]}
	w.stationRows(stations)
	w.win.Canvas().Capture()
	if first.entry.station.ID != "two" || first.text.Segments[0].Textual() != "Station Two\n" {
		t.Fatal("recycled row retained its previous station")
	}
	if first.day.Visible() {
		t.Fatal("favorites row shows an empty date heading")
	}
	if first.content.MinSize().Height > first.Size().Height {
		t.Fatal("station row is shorter than its content")
	}
	first.area.Tapped(nil)
	if p.station.ID != "two" {
		t.Fatal("recycled row played the wrong station")
	}
	first.favorite.OnTapped()
	if w.isFavorite("two") || !w.isFavorite("one") {
		t.Fatal("recycled favorite callback changed the wrong station")
	}
}
