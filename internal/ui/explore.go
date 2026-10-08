package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

const exploreRequestLimit = 20

// exploreSection browses Radio Browser and shows results in the shared station list.
type exploreSection struct {
	*fyne.Container
	window       *Window
	rows         *stationList
	name         *widget.Entry
	country      *searchCountryEntry
	countryList  *widget.List
	countryBox   *fyne.Container
	countryItems []Country
	countryMatch []Country
	countryValue Country
	countryPrior Country
	countryOpen  bool
	genre        *widget.Select
	codec        *widget.Select
	status       *widget.Label
	count        *widget.Label
	results      *fyne.Container
	timer        *time.Timer
	cancel       context.CancelFunc
	requestID    uint64
}

// searchCountryEntry adds the focus and Escape behavior used by the country picker.
type searchCountryEntry struct {
	*widget.Entry
	onFocus  func()
	onEscape func()
}

// newSearchCountryEntry creates the searchable country field.
func newSearchCountryEntry() *searchCountryEntry {
	e := &searchCountryEntry{Entry: &widget.Entry{Wrapping: fyne.TextWrap(fyne.TextTruncateClip)}}
	e.ExtendBaseWidget(e)
	return e
}

// FocusGained shows country suggestions when the field receives focus.
func (e *searchCountryEntry) FocusGained() {
	e.Entry.FocusGained()
	if e.onFocus != nil {
		e.onFocus()
	}
}

// TypedKey restores the prior country selection when Escape is pressed.
func (e *searchCountryEntry) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyEscape && e.onEscape != nil {
		e.onEscape()
		return
	}
	e.Entry.TypedKey(key)
}

// newExploreSection builds the station directory filters and result list.
func newExploreSection(w *Window) *exploreSection {
	s := &exploreSection{
		window:  w,
		rows:    newStationList(w, false),
		status:  widget.NewLabel(""),
		count:   widget.NewLabel(""),
		results: container.NewStack(),
	}
	s.name = widget.NewEntry()
	s.name.OnChanged = func(string) { s.scheduleQuery() }
	s.country = newSearchCountryEntry()
	s.country.SetPlaceHolder("Country")
	s.country.onFocus = func() {
		s.countryPrior = s.countryValue
		s.setCountryText("")
		s.showCountryOptions("")
	}
	s.country.onEscape = func() {
		s.countryValue = s.countryPrior
		s.setCountryText(s.countryValue.Name)
		s.hideCountryOptions()
	}
	s.country.OnChanged = func(query string) { s.showCountryOptions(query) }
	s.countryList = widget.NewList(
		func() int { return len(s.countryMatch) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if int(id) < len(s.countryMatch) {
				object.(*widget.Label).SetText(s.countryMatch[id].Name)
			}
		},
	)
	s.countryList.HideSeparators = true
	s.countryList.OnSelected = func(id widget.ListItemID) {
		if int(id) >= len(s.countryMatch) {
			return
		}
		s.countryValue = s.countryMatch[id]
		s.countryPrior = s.countryValue
		s.setCountryText(s.countryValue.Name)
		s.hideCountryOptions()
		s.scheduleQuery()
	}
	dropdown := widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() {
		if s.countryOpen {
			s.hideCountryOptions()
		} else {
			s.showCountryOptions("")
		}
	})
	dropdown.Importance = widget.LowImportance
	s.country.ActionItem = dropdown
	countryListContainer := container.New(searchCountryListLayout{}, s.countryList)
	countryListContainer.Hide()
	s.countryBox = container.NewVBox(s.country, countryListContainer)
	s.genre = widget.NewSelect([]string{"Any genre", "Pop", "Rock", "Jazz", "News", "Classical", "Electronic", "Talk", "Dance", "Oldies", "Ambient", "Alternative", "Metal", "Country", "Hip hop"}, nil)
	s.genre.PlaceHolder = "Genre"
	s.genre.OnChanged = func(string) { s.scheduleQuery() }
	s.codec = widget.NewSelect([]string{"Any codec", "MP3", "AAC", "AAC+", "OGG", "Opus"}, nil)
	s.codec.PlaceHolder = "Codec"
	s.codec.OnChanged = func(string) { s.scheduleQuery() }
	clear := widget.NewButtonWithIcon("", theme.CancelIcon(), func() { s.name.SetText("") })
	clear.Importance = widget.LowImportance
	searchBar := container.NewBorder(nil, nil, nil, clear, s.name)
	filters := container.NewGridWithColumns(3, s.countryBox, s.genre, s.codec)
	header := container.NewVBox(searchBar, filters, s.count, s.status)
	s.results.Objects = []fyne.CanvasObject{container.NewCenter(widget.NewLabel("Discover by station, country, genre, or codec."))}
	s.Container = container.NewBorder(header, nil, nil, nil, s.results)
	s.loadCountries()
	return s
}

// loadCountries fetches country options asynchronously from Radio Browser.
func (s *exploreSection) loadCountries() {
	if s.window.directory == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		countries, err := s.window.directory.Countries(ctx)
		if err != nil {
			return
		}
		items := make([]Country, 0, len(countries))
		for _, country := range countries {
			item := countryFromRadioBrowser(country)
			if item.Code != "" && item.Name != "" {
				items = append(items, item)
			}
		}
		fyne.Do(func() {
			s.countryItems = items
			if s.countryOpen {
				s.showCountryOptions(s.country.Text)
			}
		})
	}()
}

// setCountryText changes the entry without reopening its suggestions.
func (s *exploreSection) setCountryText(value string) {
	s.country.OnChanged = nil
	s.country.SetText(value)
	s.country.OnChanged = func(query string) { s.showCountryOptions(query) }
}

// showCountryOptions filters and displays matching country choices.
func (s *exploreSection) showCountryOptions(query string) {
	s.countryMatch = s.countryMatch[:0]
	query = strings.ToLower(strings.TrimSpace(query))
	anyCountry := Country{Name: "Any country"}
	if query == "" || strings.Contains(strings.ToLower(anyCountry.Name), query) {
		s.countryMatch = append(s.countryMatch, Country{Name: "Any country"})
	}
	for _, country := range s.countryItems {
		if strings.Contains(strings.ToLower(country.Name), query) {
			s.countryMatch = append(s.countryMatch, country)
		}
	}
	s.countryList.Refresh()
	if len(s.countryMatch) == 0 {
		s.hideCountryOptions()
		return
	}
	s.countryOpen = true
	// The list is the second child of countryBox and stays hidden until requested.
	s.countryBox.Objects[1].Show()
	s.countryBox.Refresh()
}

// hideCountryOptions closes the country suggestion dropdown.
func (s *exploreSection) hideCountryOptions() {
	s.countryOpen = false
	if len(s.countryBox.Objects) > 1 {
		s.countryBox.Objects[1].Hide()
		s.countryBox.Refresh()
	}
}

// scheduleQuery debounces edits and cancels a request made for older filters.
func (s *exploreSection) scheduleQuery() {
	s.requestID++
	requestID := s.requestID
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	query := radiobrowser.Query{Name: strings.TrimSpace(s.name.Text), Limit: exploreRequestLimit}
	if s.countryValue.Code != "" {
		query.CountryCode = s.countryValue.Code
	}
	if s.genre.Selected != "" && s.genre.Selected != "Any genre" {
		query.Tag = s.genre.Selected
	}
	if s.codec.Selected != "" && s.codec.Selected != "Any codec" {
		query.Codec = s.codec.Selected
	}
	if query.Name == "" && query.CountryCode == "" && query.Tag == "" && query.Codec == "" {
		s.rows.setStations(nil)
		s.results.Objects = []fyne.CanvasObject{container.NewCenter(widget.NewLabel("Discover by station, country, genre, or codec."))}
		s.results.Refresh()
		s.count.SetText("")
		s.status.SetText("")
		return
	}
	s.status.SetText("Searching Radio Browser…")
	s.timer = time.AfterFunc(350*time.Millisecond, func() {
		fyne.Do(func() { s.fetchStations(query, requestID) })
	})
}

// fetchStations loads matching stations and applies only the latest response.
func (s *exploreSection) fetchStations(query radiobrowser.Query, requestID uint64) {
	if s.window.directory == nil {
		s.status.SetText("Station directory unavailable.")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	s.cancel = cancel
	go func() {
		stations, err := s.window.directory.Search(ctx, query)
		cancel()
		fyne.Do(func() {
			if requestID != s.requestID {
				return
			}
			if err != nil {
				s.rows.setStations(nil)
				retry := widget.NewButton("Retry", func() { s.scheduleQuery() })
				s.results.Objects = []fyne.CanvasObject{container.NewVBox(
					container.NewCenter(widget.NewLabel("Could not load stations. Check your connection and try again.")),
					container.NewCenter(retry),
				)}
				s.count.SetText("")
				s.status.SetText("")
				s.results.Refresh()
				return
			}
			s.rows.setStations(stations)
			if len(stations) == 0 {
				s.results.Objects = []fyne.CanvasObject{container.NewCenter(widget.NewLabel("No stations found. Try different criteria or adjust the filters."))}
			} else {
				s.results.Objects = []fyne.CanvasObject{s.rows.list}
			}
			s.count.SetText(stationCount(len(stations)))
			s.status.SetText("")
			s.results.Refresh()
		})
	}()
}

type searchCountryListLayout struct{}

// MinSize keeps the country suggestions usable without expanding indefinitely.
func (searchCountryListLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 220)
}

// Layout stretches the country list to the dropdown's available width.
func (searchCountryListLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Resize(size)
	}
}

// stationCount formats a result total with the correct singular form.
func stationCount(count int) string {
	if count == 1 {
		return "1 station"
	}
	return fmt.Sprintf("%d stations", count)
}
