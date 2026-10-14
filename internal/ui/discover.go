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
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

const discoverRequestLimit = 20

type discoverGenre struct {
	label string
	tags  []string
}

// discoverGenres maps readable filter labels to alternative Radio Browser tags.
var discoverGenres = []discoverGenre{
	{label: "Pop", tags: []string{"pop", "pop music", "top 40", "dance pop"}},
	{label: "Rock", tags: []string{"rock", "classic rock", "alternative rock", "pop rock"}},
	{label: "Dance", tags: []string{"dance", "disco", "dance pop"}},
	{label: "Electronic", tags: []string{"electronic", "house", "techno", "trance"}},
	{label: "80s", tags: []string{"80s"}},
	{label: "90s", tags: []string{"90s"}},
	{label: "Jazz & Blues", tags: []string{"jazz", "smooth jazz", "blues"}},
	{label: "Classical", tags: []string{"classical", "opera", "baroque"}},
	{label: "Hip-Hop & R&B", tags: []string{"hip hop", "rap", "rnb", "r&b"}},
	{label: "Classic Hits", tags: []string{"oldies", "classic hits", "retro"}},
	{label: "70s", tags: []string{"70s"}},
	{label: "2000s", tags: []string{"2000s", "00s"}},
	{label: "Metal", tags: []string{"metal", "heavy metal"}},
	{label: "Country & Folk", tags: []string{"country", "folk"}},
	{label: "Soul & Funk", tags: []string{"soul", "funk"}},
	{label: "Chill & Ambient", tags: []string{"chillout", "ambient", "lounge", "easy listening"}},
	{label: "Latin", tags: []string{"latin", "salsa", "reggaeton", "bachata"}},
	{label: "News & Talk", tags: []string{"news", "talk", "news talk", "local news"}},
	{label: "Sports", tags: []string{"sports", "sports talk"}},
}

// discoverSection browses Radio Browser and shows results in the shared station list.
type discoverSection struct {
	*fyne.Container
	window       *Window
	rows         *stationList
	name         *widget.Entry
	country      *searchCountryEntry
	countryList  *widget.List
	countryPopup *widget.PopUp
	countryItems []Country
	countryMatch []Country
	countryValue Country
	countryPrior Country
	genre        *widget.Select
	status       *widget.Label
	count        *widget.Label
	subtitle     *widget.Label
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

// newDiscoverSection builds the station directory filters and result list.
func newDiscoverSection(w *Window) *discoverSection {
	s := &discoverSection{
		window:   w,
		rows:     newStationList(w, false),
		status:   widget.NewLabel(""),
		count:    widget.NewLabel(""),
		subtitle: widget.NewLabel("Find radio stations from around the world"),
		results:  container.NewStack(),
	}
	s.name = widget.NewEntry()
	clear := widget.NewButtonWithIcon("", theme.CancelIcon(), func() { s.name.SetText("") })
	clear.Importance = widget.LowImportance
	// Configure the action before the first refresh creates the entry renderer.
	s.name.ActionItem = clear
	s.name.SetPlaceHolder("Discover by station name…")
	s.name.SetIcon(theme.SearchIcon())
	s.name.OnChanged = func(string) { s.scheduleQuery() }
	s.country = newSearchCountryEntry()
	s.country.onFocus = func() {
		s.countryPrior = s.countryValue
		s.setCountryText("")
		s.showCountryOptions("")
	}
	s.country.onEscape = func() {
		s.countryValue = s.countryPrior
		s.setCountryText(discoverCountryLabel(s.countryValue))
		s.hideCountryOptions()
	}
	s.country.OnChanged = func(query string) { s.showCountryOptions(query) }
	s.countryList = widget.NewList(
		func() int { return len(s.countryMatch) },
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			if int(id) < len(s.countryMatch) {
				object.(*widget.Label).SetText(discoverCountryLabel(s.countryMatch[id]))
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
		s.setCountryText(discoverCountryLabel(s.countryValue))
		s.hideCountryOptions()
		s.scheduleQuery()
	}
	dropdown := widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() {
		if s.countryPopup.Visible() {
			s.hideCountryOptions()
		} else {
			s.showCountryOptions("")
		}
	})
	dropdown.Importance = widget.LowImportance
	s.country.ActionItem = dropdown
	s.country.SetPlaceHolder("Any country")
	s.countryPopup = widget.NewPopUp(s.countryList, w.window.Canvas())
	s.countryPopup.Hide()
	s.genre = widget.NewSelect(discoverGenreLabels(), nil)
	s.genre.SetSelected("Any genre")
	s.genre.OnChanged = func(string) { s.scheduleQuery() }
	filters := container.NewGridWithColumns(2, s.country, s.genre)
	s.count.Importance = widget.LowImportance
	s.status.Importance = widget.LowImportance
	s.status.Wrapping = fyne.TextWrapWord
	s.count.Hide()
	s.status.Hide()
	summary := container.NewThemeOverride(container.NewVBox(s.count, s.status),
		compactTextTheme{Theme: w.app.Settings().Theme()})
	header := container.NewPadded(container.NewVBox(s.name, filters, summary))
	s.Container = container.NewBorder(header, nil, nil, nil, s.results)
	s.loadCountries()
	if w.directory != nil {
		s.status.SetText("Loading popular stations…")
		s.status.Show()
		s.fetchStations(radiobrowser.Query{Limit: discoverRequestLimit}, s.requestID)
	}
	return s
}

// loadCountries reads cached or fetched country options outside the UI thread.
func (s *discoverSection) loadCountries() {
	if s.window.directory == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		countries, err := s.window.loadCountries(ctx)
		if err != nil {
			return
		}
		items := countriesFromRadioBrowser(countries)
		fyne.Do(func() {
			s.countryItems = items
			if s.countryPopup.Visible() {
				s.showCountryOptions(s.country.Text)
			}
		})
	}()
}

// setCountryText changes the entry without reopening its suggestions.
func (s *discoverSection) setCountryText(value string) {
	s.country.OnChanged = nil
	s.country.SetText(value)
	s.country.OnChanged = func(query string) { s.showCountryOptions(query) }
}

// showCountryOptions filters and displays matching country choices.
func (s *discoverSection) showCountryOptions(query string) {
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
	s.countryList.UnselectAll()
	if len(s.countryMatch) == 0 {
		s.hideCountryOptions()
		return
	}
	// Overlay the native list below the country field without resizing the filters.
	s.countryPopup.Resize(fyne.NewSize(s.country.Size().Width, 220))
	s.countryPopup.ShowAtRelativePosition(fyne.NewPos(0, s.country.Size().Height), s.country)
}

// hideCountryOptions closes the country suggestion dropdown.
func (s *discoverSection) hideCountryOptions() {
	s.countryPopup.Hide()
}

// scheduleQuery debounces edits and cancels a request made for older filters.
func (s *discoverSection) scheduleQuery() {
	s.requestID++
	requestID := s.requestID
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	query := radiobrowser.Query{Name: strings.TrimSpace(s.name.Text), Limit: discoverRequestLimit}
	if s.countryValue.Code != "" {
		query.CountryCode = s.countryValue.Code
	}
	query.Tags = discoverGenreTags(s.genre.Selected)
	s.count.Hide()
	if isUnfilteredDiscoverQuery(query) {
		s.status.SetText("Loading popular stations…")
	} else {
		s.status.SetText("Finding stations…")
	}
	s.status.Show()
	s.timer = time.AfterFunc(350*time.Millisecond, func() {
		fyne.Do(func() { s.fetchStations(query, requestID) })
	})
}

// discoverGenreLabels lists curated, user-facing genre names for the filter.
func discoverGenreLabels() []string {
	labels := make([]string, 0, len(discoverGenres)+1)
	labels = append(labels, "Any genre")
	for _, genre := range discoverGenres {
		labels = append(labels, genre.label)
	}
	return labels
}

// discoverGenreTags returns alternative Radio Browser tags for a selected genre.
func discoverGenreTags(label string) []string {
	for _, genre := range discoverGenres {
		if genre.label == label {
			return append([]string(nil), genre.tags...)
		}
	}
	return nil
}

// fetchStations loads matching stations and applies only the latest response.
func (s *discoverSection) fetchStations(query radiobrowser.Query, requestID uint64) {
	if s.window.directory == nil {
		s.status.SetText("Station directory unavailable.")
		s.status.Show()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	s.cancel = cancel
	go func() {
		var stations []radio.Station
		var err error
		if isUnfilteredDiscoverQuery(query) {
			stations, err = s.window.directory.Popular(ctx, query.Limit)
		} else {
			stations, err = s.window.directory.Search(ctx, query)
		}
		cancel()
		fyne.Do(func() {
			if requestID != s.requestID {
				return
			}
			if err != nil {
				s.rows.setStations(nil)
				retry := widget.NewButton("Retry", func() { s.scheduleQuery() })
				s.results.Objects = []fyne.CanvasObject{container.NewVBox(
					discoverEmptyState("Could not load stations", "Check your connection and try again."),
					container.NewCenter(retry),
				)}
				s.count.SetText("")
				s.count.Hide()
				s.status.SetText("")
				s.status.Hide()
				s.results.Refresh()
				return
			}
			s.rows.setStations(stations)
			if len(stations) == 0 {
				s.results.Objects = []fyne.CanvasObject{discoverEmptyState("No stations found", "Try a different name or adjust the filters.")}
			} else {
				s.results.Objects = []fyne.CanvasObject{s.rows.list}
			}
			if len(stations) == 0 {
				s.count.Hide()
			} else {
				if isUnfilteredDiscoverQuery(query) {
					s.count.SetText(fmt.Sprintf("Most popular %d stations", len(stations)))
				} else {
					s.count.SetText(stationCount(len(stations)))
				}
				s.count.Show()
			}
			s.status.SetText("")
			s.status.Hide()
			s.results.Refresh()
		})
	}()
}

// isUnfilteredDiscoverQuery reports whether the query has no active filters.
func isUnfilteredDiscoverQuery(query radiobrowser.Query) bool {
	return query.Name == "" && query.CountryCode == "" && len(query.Tags) == 0 && query.Codec == ""
}

// discoverCountryLabel displays the country flag beside its localized name.
func discoverCountryLabel(country Country) string {
	if country.Code == "" {
		return country.Name
	}
	return countryOptionFlag(country.Code) + " " + country.Name
}

// discoverEmptyState presents a compact message when no result list is available.
func discoverEmptyState(title, description string) fyne.CanvasObject {
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel(description)
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewVBox(widget.NewIcon(theme.SearchIcon()), heading, detail))
}

// stationCount formats a result total with the correct singular form.
func stationCount(count int) string {
	if count == 1 {
		return "Showing 1 station"
	}
	return fmt.Sprintf("Showing %d stations", count)
}
