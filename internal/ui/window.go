package ui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	appcache "go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/directory/radiobrowser"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/version"
)

type page int

const (
	pageDiscover page = iota
	pageFavorites
	pageRecent
	pageSearch
	pagePreferences
)

type Window struct {
	app                                                  fyne.App
	win                                                  fyne.Window
	player                                               radio.Player
	directory                                            radiobrowser.Directory
	store                                                *storage.Store
	settings                                             storage.Settings
	selected                                             radio.Station
	regionSetupNeeded                                    bool
	regionSuggestionCode                                 string
	regionSelect                                         *searchableEntry
	regionList                                           *widget.List
	regionListContainer                                  *fyne.Container
	regionSetupPage                                      *fyne.Container
	regionMatches                                        []string
	regionOptionsVisible                                 bool
	regionUpdating                                       bool
	regionPreviousChoice                                 string
	regionStatus                                         *widget.Label
	regionContinue                                       *widget.Button
	countries                                            []radiobrowser.Country
	navigation, playerBar                                *fyne.Container
	headerMain, headerPreferences                        *fyne.Container
	title                                                *widget.Label
	artist                                               *widget.Label
	playerArtwork                                        *fyne.Container
	playerArt                                            *canvas.Image
	playerArtID                                          string
	play                                                 *widget.Button
	volumeMuteButton                                     *widget.Button
	volumeSlider                                         *widget.Slider
	volumeLabel                                          *widget.Label
	expandedPlayer                                       *fyne.Container
	playerExpanded                                       bool
	volumeMuted                                          bool
	volumeBeforeMute                                     float64
	navDiscover, navFavorites, navRecent                 *navItem
	body                                                 *fyne.Container
	resultCount                                          *widget.Label
	resultHeading                                        *widget.Label
	discoveryHeading                                     *widget.Label
	directoryStatus                                      *widget.Label
	remoteResults                                        *fyne.Container
	searchName                                           *widget.Entry
	searchCountry                                        *searchableEntry
	searchCountryList                                    *widget.List
	searchCountryListContainer                           *fyne.Container
	searchCountryMatches                                 []string
	searchCountryOptionsVisible                          bool
	searchCountryUpdating                                bool
	searchCountryPreviousChoice                          string
	searchTag, searchCodec                               *widget.Select
	searchFilters                                        *fyne.Container
	countryOptions                                       []string
	searchCountryValue, searchTagValue, searchCodecValue string
	directoryStations                                    []radio.Station
	visibleStationRows                                   []*stationTapArea
	query                                                string
	page                                                 page
	pageBeforePreferences                                page
	recent                                               []recentStation
	hasTray                                              bool
	tray                                                 desktop.App
	done                                                 chan struct{}
	eventsDone                                           chan struct{}
	closeOnce                                            sync.Once
	closeErr                                             error
	directoryMu                                          sync.Mutex
	directorySeq                                         uint64
	directoryCancel                                      context.CancelFunc
	searchTimer                                          *time.Timer
	faviconClient                                        *http.Client
	cacheStore                                           *appcache.Store
	faviconContext                                       context.Context
	faviconCancel                                        context.CancelFunc
	faviconSlots                                         chan struct{}
	faviconCache                                         map[string]stationIcon
	stationIcons                                         map[string]stationIcon
	faviconLoading                                       map[string][]*canvas.Image
	faviconFailed                                        map[string]bool
	faviconMissingLogged                                 map[string]bool
}

type recentStation = storage.RecentStation

type stationRowDetails struct {
	genre    string
	metadata string
	codec    string
}

func restoreSelectedStation(id string, stations []radio.Station, recent []recentStation) radio.Station {
	if id == "" {
		return radio.Station{}
	}
	for _, station := range stations {
		if station.ID == id {
			return station
		}
	}
	for _, entry := range recent {
		if entry.Station.ID == id {
			return entry.Station
		}
	}
	return radio.Station{}
}

type stationIcon struct {
	image    image.Image
	resource fyne.Resource
}

type fixedHeightLayout struct {
	height float32
}

func (l fixedHeightLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, l.height)
}

func (l fixedHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, object := range objects {
		object.Resize(size)
	}
}

type searchableEntry struct {
	*widget.Entry
	focused  bool
	onFocus  func()
	onEscape func()
}

func newSearchableEntry() *searchableEntry {
	e := &searchableEntry{
		Entry: &widget.Entry{Wrapping: fyne.TextWrap(fyne.TextTruncateClip)},
	}
	e.ExtendBaseWidget(e)
	return e
}

func (e *searchableEntry) FocusGained() {
	e.Entry.FocusGained()
	e.focused = true
	if e.onFocus != nil {
		e.onFocus()
	}
}

func (e *searchableEntry) FocusLost() {
	e.focused = false
	e.Entry.FocusLost()
}

func (e *searchableEntry) TypedKey(key *fyne.KeyEvent) {
	if key.Name == fyne.KeyEscape && e.onEscape != nil {
		e.onEscape()
		return
	}
	e.Entry.TypedKey(key)
}

const (
	maxStationIconDimension    = 4096
	maxStationIconPixels       = 16 * 1024 * 1024
	maxStationIconCacheSize    = 4 << 20
	stationArtworkSize         = 52
	stationArtworkCornerRadius = 8
	stationArtworkTextGap      = 8
	headerLogoWidth            = 170
	headerLogoHeight           = 47
	radioBrowserCacheTTL       = 24 * time.Hour
	stationIconCacheTTL        = 30 * 24 * time.Hour
)

func New(a fyne.App, player radio.Player, directories ...radiobrowser.Directory) *Window {
	a.Settings().SetTheme(brandTheme{Theme: a.Settings().Theme()})
	w := &Window{
		app: a, win: a.NewWindow("FyneTune"), player: player, store: storage.New(a),
		done: make(chan struct{}), eventsDone: make(chan struct{}),
		cacheStore: appcache.New(a.Cache()),
	}
	if len(directories) > 0 {
		w.directory = directories[0]
	}
	w.faviconContext, w.faviconCancel = context.WithCancel(context.Background())
	w.faviconClient = &http.Client{Timeout: 8 * time.Second}
	w.faviconSlots = make(chan struct{}, 4)
	w.faviconCache = make(map[string]stationIcon)
	w.stationIcons = make(map[string]stationIcon)
	w.faviconLoading = make(map[string][]*canvas.Image)
	w.faviconFailed = make(map[string]bool)
	w.faviconMissingLogged = make(map[string]bool)
	a.SetIcon(appIconResource)
	settings, err := w.store.Load()
	if err != nil {
		dialog.ShowError(err, w.win)
	}
	w.settings = settings
	w.recent, err = w.store.LoadRecent()
	if err != nil {
		slog.Warn("could not load recent stations", "error", err)
		w.recent = nil
	}
	needsRegionSetup := w.settings.DiscoveryCountry == ""
	if w.settings.DiscoveryCountry == "" {
		if code := systemLocaleCountryCode(); code != "" {
			w.settings.DiscoveryCountry = "@" + code
			w.regionSuggestionCode = code
		} else {
			w.settings.DiscoveryCountry = "*"
		}
	}
	w.regionSetupNeeded = needsRegionSetup
	w.countryOptions = []string{"Any country"}
	w.volumeBeforeMute = settings.Volume
	w.player.SetVolume(settings.Volume)
	w.player.SetReconnect(settings.Reconnect)
	w.win.Resize(fyne.NewSize(480, 800))
	w.build()
	w.setAboutMenu()
	go func() {
		defer close(w.eventsDone)
		w.events()
	}()
	w.selected = restoreSelectedStation(settings.Selected, settings.Stations, w.recent)
	if w.selected.ID == "" && len(settings.Stations) > 0 {
		w.selected = settings.Stations[0]
	}
	w.updateNavigation()
	w.refreshPage()
	w.updatePlayerBar()
	w.updateTray()
	w.loadCountries()
	if w.page == pageDiscover && !w.regionSetupNeeded {
		w.loadPopular()
	}
	if settings.AutoPlay && w.selected.ID != "" {
		w.playStation(w.selected)
	}
	if w.hasTray {
		w.win.SetCloseIntercept(func() { w.win.Hide() })
	}
	return w
}

func iconButton(icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

func (w *Window) favoriteButton(station radio.Station) fyne.CanvasObject {
	icon := favoriteRowOutline
	if w.isFavorite(station.ID) {
		icon = favoriteRowFilled
	}
	button := iconButton(icon, func() { w.toggleFavorite(station) })
	return container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(favoriteTouchSize),
		container.NewThemeOverride(button, favoriteButtonTheme{Theme: w.app.Settings().Theme()})))
}

func (w *Window) setVolume(volume float64) {
	if volume < 0 {
		volume = 0
	} else if volume > 1 {
		volume = 1
	}
	if volume > 0 {
		w.volumeBeforeMute = volume
		w.volumeMuted = false
	} else if w.settings.Volume > 0 {
		w.volumeBeforeMute = w.settings.Volume
	}
	w.settings.Volume = volume
	_ = w.store.SaveSettings(w.settings)
	w.applyVolume()
}

func (w *Window) toggleMute() {
	if w.volumeMuted || w.settings.Volume == 0 {
		w.volumeMuted = false
		if w.settings.Volume == 0 {
			w.settings.Volume = w.volumeBeforeMute
			if w.settings.Volume <= 0 {
				w.settings.Volume = 0.5
			}
			_ = w.store.SaveSettings(w.settings)
		}
	} else {
		w.volumeBeforeMute = w.settings.Volume
		w.volumeMuted = true
	}
	w.applyVolume()
}

func (w *Window) applyVolume() {
	volume := w.settings.Volume
	if w.volumeMuted {
		volume = 0
	}
	w.player.SetVolume(volume)
	w.updateVolumeControls()
}

func (w *Window) updateVolumeControls() {
	muted := w.volumeMuted || w.settings.Volume == 0
	if w.volumeMuteButton != nil {
		if muted {
			w.volumeMuteButton.SetIcon(theme.VolumeMuteIcon())
		} else {
			w.volumeMuteButton.SetIcon(theme.VolumeUpIcon())
		}
	}
	if w.volumeSlider != nil {
		w.volumeSlider.SetValue(w.settings.Volume)
	}
	if w.volumeLabel != nil {
		volume := w.settings.Volume
		if muted {
			volume = 0
		}
		w.volumeLabel.SetText(fmt.Sprintf("%d%%", int(volume*100+0.5)))
	}
}

func (w *Window) build() {
	logo := canvas.NewImageFromResource(logoResource)
	logo.FillMode = canvas.ImageFillContain
	brand := container.NewCenter(container.NewGridWrap(fyne.NewSize(headerLogoWidth, headerLogoHeight), logo))
	settings := iconButton(theme.SettingsIcon(), w.openPreferences)
	w.headerMain = container.NewBorder(nil, nil, brand, settings, nil)
	back := iconButton(theme.NavigateBackIcon(), w.closePreferences)
	preferencesTitle := widget.NewLabelWithStyle("Preferences", fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	w.headerPreferences = container.NewBorder(nil, nil, back, nil, preferencesTitle)
	w.headerPreferences.Hide()
	header := container.NewStack(w.headerMain, w.headerPreferences)

	w.body = container.NewStack()
	w.title = widget.NewLabelWithStyle("Choose a station", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.title.Truncation = fyne.TextTruncateEllipsis
	w.artist = widget.NewLabel("Not playing")
	w.artist.Truncation = fyne.TextTruncateEllipsis
	fallbackArt := canvas.NewImageFromResource(appIconResource)
	fallbackArt.FillMode = canvas.ImageFillContain
	fallbackArt.CornerRadius = stationArtworkCornerRadius
	fallbackArt.SetMinSize(fyne.NewSquareSize(stationArtworkSize))
	fallbackArt.Resize(fyne.NewSquareSize(stationArtworkSize))
	w.playerArtwork = stationArtworkFrame(fallbackArt)
	w.play = iconButton(theme.MediaPlayIcon(), w.togglePlay)
	w.play.Importance = widget.HighImportance
	playControl := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(playerControlSize),
		container.NewThemeOverride(w.play, playerControlTheme{Theme: w.app.Settings().Theme()})))
	w.volumeSlider = widget.NewSlider(0, 1)
	w.volumeSlider.Step = 0.01
	w.volumeSlider.Value = w.settings.Volume
	w.volumeSlider.OnChanged = w.setVolume
	w.volumeLabel = widget.NewLabel("")
	w.volumeLabel.Alignment = fyne.TextAlignTrailing
	w.volumeMuteButton = iconButton(theme.VolumeMuteIcon(), w.toggleMute)
	volumeControl := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(playerControlSize),
		container.NewThemeOverride(w.volumeMuteButton, playerControlTheme{Theme: w.app.Settings().Theme()})))
	volumeValue := container.NewCenter(container.NewGridWrap(fyne.NewSize(playerVolumeValueWidth, playerControlSize), w.volumeLabel))
	w.expandedPlayer = container.NewBorder(
		nil, nil,
		volumeControl,
		volumeValue,
		w.volumeSlider,
	)
	w.expandedPlayer.Hide()
	stationTitle := container.NewThemeOverride(w.title, stationRowTextTheme{Theme: w.app.Settings().Theme()})
	track := container.NewThemeOverride(w.artist, playerTrackTheme{Theme: w.app.Settings().Theme()})
	playerText := container.New(layout.NewCustomPaddedVBoxLayout(playerTextGap), stationTitle, track)
	centeredText := container.NewVBox(layout.NewSpacer(), playerText, layout.NewSpacer())
	artworkGap := canvas.NewRectangle(color.Transparent)
	artworkGap.SetMinSize(fyne.NewSize(stationArtworkTextGap, 0))
	artwork := container.New(layout.NewCustomPaddedHBoxLayout(0), container.NewCenter(w.playerArtwork), artworkGap)
	barContent := container.NewBorder(nil, nil, artwork, nil, centeredText)
	barTap := newStationTapArea(barContent, w.toggleExpandedPlayer, nil)
	playerBar := container.NewVBox(
		widget.NewSeparator(),
		container.NewBorder(nil, nil, nil, playControl, barTap),
		w.expandedPlayer,
	)
	w.playerBar = playerBar
	w.navDiscover = newNavItem("Discover", theme.HomeIcon(), nil, func() { w.showPage(pageDiscover) })
	w.navFavorites = newNavItem("Favorites", favoriteOutlineResource, favoriteRowFilled, func() { w.showPage(pageFavorites) })
	w.navRecent = newNavItem("Recent", theme.HistoryIcon(), nil, func() { w.showPage(pageRecent) })
	nav := container.NewGridWithColumns(3, w.navDiscover, w.navFavorites, w.navRecent)
	w.navigation = nav
	footer := container.NewVBox(
		container.NewPadded(playerBar),
		widget.NewSeparator(),
		container.NewPadded(nav),
	)
	if w.regionSetupNeeded {
		w.playerBar.Hide()
		w.navigation.Hide()
	}
	w.win.SetContent(container.NewBorder(container.NewPadded(header), footer, nil, nil, w.body))
	if d, ok := w.app.(desktop.App); ok {
		w.hasTray = true
		w.tray = d
		d.SetSystemTrayIcon(theme.NewThemedResource(appIconResource))
		w.updateTray()
	}
}

func (w *Window) showPage(p page) {
	if w.regionSetupNeeded {
		return
	}
	w.setPage(p)
}

func (w *Window) openPreferences() {
	if w.page != pagePreferences {
		w.pageBeforePreferences = w.page
	}
	w.setPage(pagePreferences)
	w.headerMain.Hide()
	w.headerPreferences.Show()
}

func (w *Window) closePreferences() {
	page := w.pageBeforePreferences
	if page == pagePreferences {
		page = pageDiscover
	}
	w.showPage(page)
	w.headerPreferences.Hide()
	w.headerMain.Show()
}

func (w *Window) setPage(p page) {
	w.cancelDirectoryRequest()
	if w.searchTimer != nil {
		w.searchTimer.Stop()
	}
	w.page = p
	w.query = ""
	w.updateNavigation()
	w.refreshPage()
	if p == pageDiscover {
		w.loadPopular()
	}
}

func (w *Window) updateNavigation() {
	current := w.page
	if current == pageSearch {
		current = pageDiscover
	}
	w.navDiscover.SetSelected(current == pageDiscover)
	w.navFavorites.SetSelected(current == pageFavorites)
	w.navRecent.SetSelected(current == pageRecent)
}

func (w *Window) refreshPage() {
	w.visibleStationRows = nil
	var content *fyne.Container
	if w.regionSetupNeeded {
		content = w.regionWelcomePage()
	} else {
		switch w.page {
		case pageFavorites:
			content = w.stationPage("Favorites", "Your saved radio stations", w.settings.Stations)
		case pageRecent:
			content = w.recentPage()
		case pageSearch:
			content = w.searchPage()
		case pagePreferences:
			content = w.preferencesPage()
		default:
			content = w.discoverPage()
		}
	}
	w.body.Objects = []fyne.CanvasObject{container.NewVScroll(content)}
	w.body.Refresh()
}

func (w *Window) discoverPage() *fyne.Container {
	search := widget.NewEntry()
	search.SetPlaceHolder("Search stations, genres, countries…")
	search.SetText(w.query)
	search.OnChanged = func(value string) {
		w.query = value
		w.cancelDirectoryRequest()
		w.page = pageSearch
		w.refreshPage()
		if w.searchName != nil {
			w.searchName.SetText(value)
		}
		w.scheduleDirectorySearch()
	}
	w.directoryStatus = widget.NewLabel("Loading popular stations…")
	w.remoteResults = container.NewVBox()
	w.discoveryHeading = widget.NewLabelWithStyle(w.popularHeading(), fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(
		widget.NewLabelWithStyle("Discover", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		search,
		w.discoveryHeading,
		w.directoryStatus,
		w.remoteResults,
	)
}

func (w *Window) regionWelcomePage() *fyne.Container {
	options := w.regionOptions()
	w.regionSelect = newSearchableEntry()
	w.regionSelect.SetPlaceHolder("Choose or search for a country")
	w.regionSelect.SetText(w.initialRegionOption(options))
	w.regionPreviousChoice = w.regionSelect.Text
	w.regionSelect.onFocus = func() {
		if selected := strings.TrimSpace(w.regionSelect.Text); selected != "" {
			w.regionPreviousChoice = selected
		}
		w.setRegionText("")
		w.showRegionOptions("")
	}
	w.regionSelect.onEscape = func() {
		w.setRegionText(w.regionPreviousChoice)
		w.hideRegionOptions()
	}
	w.regionList = widget.NewList(
		func() int { return len(w.regionMatches) },
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < len(w.regionMatches) {
				item.(*widget.Label).SetText(w.regionMatches[id])
			}
		},
	)
	w.regionList.HideSeparators = true
	w.regionList.OnSelected = func(id widget.ListItemID) {
		if id >= len(w.regionMatches) {
			return
		}
		w.regionPreviousChoice = w.regionMatches[id]
		w.setRegionText(w.regionPreviousChoice)
		w.hideRegionOptions()
	}
	w.regionSelect.OnChanged = func(query string) {
		if !w.regionUpdating {
			w.showRegionOptions(query)
		}
	}
	dropdown := widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() {
		if w.regionOptionsVisible {
			w.hideRegionOptions()
		} else {
			w.showRegionOptions("")
		}
	})
	dropdown.Importance = widget.LowImportance
	w.regionSelect.ActionItem = dropdown
	w.regionListContainer = container.New(fixedHeightLayout{height: 220}, w.regionList)
	w.regionListContainer.Hide()

	icon := canvas.NewImageFromResource(appIconResource)
	icon.FillMode = canvas.ImageFillContain
	continueButton := widget.NewButton("Continue", w.completeRegionSetup)
	continueButton.Importance = widget.HighImportance
	continueButton.Disable()
	w.regionContinue = continueButton
	w.regionStatus = widget.NewLabel("Loading country list…")
	privacyNote := widget.NewRichText(&widget.TextSegment{
		Style: widget.RichTextStyle{
			Alignment: fyne.TextAlignCenter,
			ColorName: theme.ColorNameForeground,
			SizeName:  theme.SizeNameText,
		},
		Text: "Suggested from your device’s regional settings.\nNo location data is used.",
	})
	privacyNote.Wrapping = fyne.TextWrapWord
	w.regionSetupPage = container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Welcome to FyneTune", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(80, 80), icon)),
		widget.NewLabelWithStyle("Choose your radio region", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		w.regionSelect,
		w.regionListContainer,
		w.regionStatus,
		continueButton,
		privacyNote,
	))
	return w.regionSetupPage
}

func (w *Window) regionOptions() []string {
	options := []string{"Any country"}
	for _, country := range w.countries {
		if country.Name != "" && !contains(options, country.Name) {
			options = append(options, country.Name)
		}
	}
	return options
}

func (w *Window) setRegionText(value string) {
	if w.regionSelect == nil {
		return
	}
	w.regionUpdating = true
	w.regionSelect.SetText(value)
	w.regionUpdating = false
}

func (w *Window) showRegionOptions(query string) {
	if w.regionSelect == nil || w.regionList == nil || w.regionListContainer == nil {
		return
	}
	w.regionMatches = filterRegionOptions(w.regionOptions(), query)
	w.regionList.Refresh()
	if len(w.regionMatches) == 0 {
		w.hideRegionOptions()
		return
	}
	w.regionOptionsVisible = true
	w.regionListContainer.Show()
	if w.regionSetupPage != nil {
		w.regionSetupPage.Refresh()
	} else {
		w.regionListContainer.Refresh()
	}
}

func filterRegionOptions(options []string, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]string(nil), options...)
	}
	filtered := make([]string, 0, len(options))
	for _, option := range options {
		if strings.Contains(strings.ToLower(option), query) {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func (w *Window) hideRegionOptions() {
	w.regionOptionsVisible = false
	if w.regionListContainer != nil {
		w.regionListContainer.Hide()
	}
	if w.regionSetupPage != nil {
		w.regionSetupPage.Refresh()
	}
}

func (w *Window) initialRegionOption(options []string) string {
	if w.regionSuggestionCode != "" {
		if name := w.countryNameForCode(w.regionSuggestionCode); name != "" {
			return name
		}
		return w.regionSuggestionCode
	}
	if len(options) > 0 {
		return options[0]
	}
	return ""
}

func (w *Window) completeRegionSetup() {
	w.hideRegionOptions()
	w.settings.DiscoveryCountry = discoveryCountryForSelection(
		w.regionSelect.Text, w.regionSuggestionCode, w.countries,
	)
	if err := w.store.SaveSettings(w.settings); err != nil {
		dialog.ShowError(err, w.win)
		return
	}
	w.regionSetupNeeded = false
	if w.navigation != nil {
		w.navigation.Show()
	}
	if w.playerBar != nil {
		w.playerBar.Show()
	}
	w.refreshPage()
	w.loadPopular()
}

func discoveryCountryForSelection(selected, suggestedCode string, countries []radiobrowser.Country) string {
	selected = strings.TrimSpace(selected)
	switch {
	case strings.EqualFold(selected, "Any country") || selected == "":
		return "*"
	case strings.EqualFold(selected, suggestedCode) && suggestedCode != "":
		return "@" + suggestedCode
	default:
		for _, country := range countries {
			if strings.EqualFold(country.Name, selected) && country.Code != "" {
				return "@" + country.Code
			}
		}
		return selected
	}
}

func (w *Window) setDirectoryStatus(text string) {
	if w.directoryStatus == nil {
		return
	}
	w.directoryStatus.SetText(text)
	if text == "" {
		w.directoryStatus.Hide()
	} else {
		w.directoryStatus.Show()
	}
}

func (w *Window) searchPage() *fyne.Container {
	w.searchName = widget.NewEntry()
	w.searchName.SetPlaceHolder("Search stations, genres, countries…")
	w.searchName.SetText(w.query)
	w.searchName.OnChanged = func(value string) {
		w.query = value
		if w.resultHeading != nil {
			if value == "" {
				w.resultHeading.SetText("Search stations")
			} else {
				w.resultHeading.SetText("Results for “" + value + "”")
			}
		}
		w.scheduleDirectorySearch()
	}

	w.searchCountry = newSearchableEntry()
	w.searchCountry.SetPlaceHolder("Country")
	w.searchCountry.SetText(w.searchCountryValue)
	w.searchCountryPreviousChoice = w.searchCountryValue
	w.searchCountry.onFocus = func() {
		w.searchCountryPreviousChoice = w.searchCountryValue
		w.setSearchCountryText("")
		w.showSearchCountryOptions("")
	}
	w.searchCountry.onEscape = func() {
		w.setSearchCountryText(w.searchCountryPreviousChoice)
		w.hideSearchCountryOptions()
	}
	w.searchCountryList = widget.NewList(
		func() int { return len(w.searchCountryMatches) },
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id < len(w.searchCountryMatches) {
				item.(*widget.Label).SetText(w.searchCountryMatches[id])
			}
		},
	)
	w.searchCountryList.HideSeparators = true
	w.searchCountryList.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(w.searchCountryMatches) {
			return
		}
		selected := w.searchCountryMatches[id]
		if selected == "Any country" {
			w.searchCountryValue = ""
			w.setSearchCountryText("")
		} else {
			w.searchCountryValue = selected
			w.setSearchCountryText(selected)
		}
		w.searchCountryPreviousChoice = w.searchCountryValue
		w.hideSearchCountryOptions()
		w.scheduleDirectorySearch()
	}
	w.searchCountry.OnChanged = func(query string) {
		if !w.searchCountryUpdating {
			w.showSearchCountryOptions(query)
		}
	}
	countryDropdown := widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() {
		if w.searchCountryOptionsVisible {
			w.hideSearchCountryOptions()
		} else {
			w.showSearchCountryOptions("")
		}
	})
	countryDropdown.Importance = widget.LowImportance
	w.searchCountry.ActionItem = countryDropdown
	w.searchCountryListContainer = container.New(fixedHeightLayout{height: 220}, w.searchCountryList)
	w.searchCountryListContainer.Hide()
	countryPicker := container.NewVBox(w.searchCountry, w.searchCountryListContainer)
	w.searchTag = widget.NewSelect([]string{"Any genre", "Pop", "Rock", "Jazz", "News", "Classical", "Electronic", "Talk", "Dance", "Oldies", "Ambient", "Alternative", "Metal", "Country", "Hip hop"}, nil)
	w.searchTag.PlaceHolder = "Genre"
	w.searchTag.SetSelected(w.searchTagValue)
	w.searchTag.OnChanged = func(value string) {
		w.searchTagValue = value
		w.scheduleDirectorySearch()
	}
	w.searchCodec = widget.NewSelect([]string{"Any codec", "MP3", "AAC", "AAC+", "OGG", "Opus"}, nil)
	w.searchCodec.PlaceHolder = "Codec"
	w.searchCodec.SetSelected(w.searchCodecValue)
	w.searchCodec.OnChanged = func(value string) {
		w.searchCodecValue = value
		w.scheduleDirectorySearch()
	}

	back := iconButton(theme.NavigateBackIcon(), func() { w.showPage(pageDiscover) })
	clear := iconButton(theme.CancelIcon(), func() { w.searchName.SetText("") })
	searchBar := container.NewBorder(nil, nil, widget.NewIcon(theme.SearchIcon()), clear, w.searchName)
	searchRow := container.NewBorder(nil, nil, back, nil, searchBar)
	w.searchFilters = container.NewGridWithColumns(3, countryPicker, w.searchTag, w.searchCodec)
	w.directoryStatus = widget.NewLabel("Enter a search or filter to find stations.")
	w.remoteResults = container.NewVBox()
	w.resultCount = widget.NewLabel("")
	if w.query == "" {
		w.resultHeading = widget.NewLabelWithStyle("Search stations", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	} else {
		w.resultHeading = widget.NewLabelWithStyle("Results for “"+w.query+"”", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	return container.NewVBox(
		searchRow,
		w.searchFilters,
		w.resultHeading,
		w.resultCount,
		w.directoryStatus,
		w.remoteResults,
	)
}

func (w *Window) searchCountryOptions() []string {
	options := []string{"Any country"}
	for _, country := range w.countryOptions {
		if country != "Any country" && !contains(options, country) {
			options = append(options, country)
		}
	}
	return options
}

func (w *Window) setSearchCountryText(value string) {
	if w.searchCountry == nil {
		return
	}
	w.searchCountryUpdating = true
	w.searchCountry.SetText(value)
	w.searchCountryUpdating = false
}

func (w *Window) showSearchCountryOptions(query string) {
	if w.searchCountry == nil || w.searchCountryList == nil || w.searchCountryListContainer == nil {
		return
	}
	w.searchCountryMatches = filterRegionOptions(w.searchCountryOptions(), query)
	w.searchCountryList.Refresh()
	if len(w.searchCountryMatches) == 0 {
		w.hideSearchCountryOptions()
		return
	}
	w.searchCountryOptionsVisible = true
	w.searchCountryListContainer.Show()
	if w.searchFilters != nil {
		w.searchFilters.Refresh()
	} else {
		w.searchCountryListContainer.Refresh()
	}
}

func (w *Window) hideSearchCountryOptions() {
	w.searchCountryOptionsVisible = false
	if w.searchCountryListContainer != nil {
		w.searchCountryListContainer.Hide()
	}
	if w.searchFilters != nil {
		w.searchFilters.Refresh()
	}
}

func (w *Window) stationPage(title, subtitle string, stations []radio.Station) *fyne.Container {
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	add := iconButton(theme.ContentAddIcon(), func() { w.stationDialog(nil) })
	header := container.NewBorder(nil, nil, nil, add, heading)
	if len(stations) == 0 {
		return container.NewVBox(header, w.emptyState(favoriteOutlineResource, "No favorites yet", "Save a station from Discover to keep it here."))
	}
	return container.NewVBox(header, widget.NewLabel(subtitle), w.stationRows(stations))
}

func (w *Window) stationRows(stations []radio.Station) *fyne.Container {
	rows := container.NewVBox()
	for _, station := range stations {
		s := station
		favorite := w.favoriteButton(s)
		row := w.stationRow(s, stationMeta(s, ""), favorite, func() { w.playStation(s) }, func() { w.stationMenu(s) })
		rows.Add(row)
	}
	return rows
}

func (w *Window) recentPage() *fyne.Container {
	heading := widget.NewLabelWithStyle("Recent", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	if len(w.recent) == 0 {
		return container.NewVBox(heading, w.emptyState(theme.HistoryIcon(), "Nothing played yet", "Stations you play will appear here."))
	}
	rows := container.NewVBox(heading)
	previousDay := ""
	for _, recent := range w.recent {
		day := recent.PlayedAt.Format("2006-01-02")
		if day != previousDay {
			rows.Add(widget.NewLabelWithStyle(recentDayLabel(recent.PlayedAt), fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
			previousDay = day
		}
		station := recent.Station
		favorite := w.favoriteButton(station)
		rows.Add(w.stationRow(station, stationRowDetails{metadata: "Played " + recent.PlayedAt.Format("15:04")}, favorite, func() { w.playStation(station) }, func() { w.stationMenu(station) }))
	}
	return rows
}

func recentDayLabel(playedAt time.Time) string {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(playedAt.Year(), playedAt.Month(), playedAt.Day(), 0, 0, 0, 0, playedAt.Location())
	switch {
	case day.Equal(today):
		return "Today"
	case day.Equal(today.AddDate(0, 0, -1)):
		return "Yesterday"
	default:
		return playedAt.Format("Monday, 2 January")
	}
}

func (w *Window) stationRow(station radio.Station, detail stationRowDetails, trailing fyne.CanvasObject, onTapped, onLongTapped func()) fyne.CanvasObject {
	textView := stationRowDetailView(detail)
	textView.Segments = append([]widget.RichTextSegment{
		&widget.TextSegment{Text: station.Name + "\n", Style: widget.RichTextStyleStrong},
	}, textView.Segments...)
	text := container.NewThemeOverride(textView, stationRowTextTheme{Theme: w.app.Settings().Theme()})
	artwork := container.NewCenter(stationArtworkFrame(w.stationImage(station)))
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(stationArtworkTextGap, 0))
	icon := container.New(layout.NewCustomPaddedHBoxLayout(0), artwork, gap)
	content := container.NewBorder(nil, nil, icon, nil, text)
	if onTapped != nil {
		area := newStationTapArea(content, onTapped, onLongTapped)
		area.stationID = station.ID
		area.SetHighlighted(w.stationIsActive(station.ID))
		w.visibleStationRows = append(w.visibleStationRows, area)
		row := container.NewBorder(nil, nil, nil, trailing, area)
		return container.NewPadded(row)
	}
	return container.NewPadded(container.NewBorder(nil, nil, icon, trailing, text))
}

func stationMeta(station radio.Station, extra string) stationRowDetails {
	details := stationRowDetails{}
	if len(station.Tags) > 0 {
		details.genre = station.Tags[0]
	}
	parts := make([]string, 0, 4)
	if station.Country != "" {
		parts = append(parts, station.Country)
	}
	technical := make([]string, 0, 2)
	codec := strings.TrimSpace(station.Codec)
	if codec != "" && !strings.EqualFold(codec, "unknown") {
		technical = append(technical, strings.ToUpper(codec))
	}
	if station.Bitrate > 0 {
		technical = append(technical, fmt.Sprintf("%d kbps", station.Bitrate))
	}
	details.codec = strings.Join(technical, " · ")
	if extra != "" {
		parts = append(parts, extra)
	}
	if len(parts) == 0 && details.genre == "" {
		parts = append(parts, stationHost(station.URL))
	}
	details.metadata = strings.Join(parts, " · ")
	return details
}

func stationRowDetailView(details stationRowDetails) *widget.RichText {
	parts := make([]string, 0, 2)
	if details.genre != "" {
		parts = append(parts, details.genre)
	}
	if details.metadata != "" {
		parts = append(parts, details.metadata)
	}
	text := strings.Join(parts, " · ")
	if details.codec != "" {
		if text != "" {
			text += "\n"
		}
		text += details.codec
	}
	style := widget.RichTextStyleInline
	style.ColorName = stationMetadataColorName
	style.SizeName = stationMetadataSizeName
	view := widget.NewRichText(&widget.TextSegment{Text: text, Style: style})
	view.Truncation = fyne.TextTruncateEllipsis
	return view
}

func (w *Window) stationIsActive(id string) bool {
	if id == "" || id != w.selected.ID {
		return false
	}
	switch w.player.State() {
	case radio.StateConnecting, radio.StateBuffering, radio.StatePlaying, radio.StateReconnecting:
		return true
	default:
		return false
	}
}

func (w *Window) emptyState(icon fyne.Resource, title, message string) fyne.CanvasObject {
	mark := widget.NewIcon(icon)
	mark.Resize(fyne.NewSquareSize(42))
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	detail := widget.NewLabel(message)
	detail.Alignment = fyne.TextAlignCenter
	detail.Wrapping = fyne.TextWrapWord
	return container.NewCenter(container.NewVBox(mark, heading, detail))
}

func (w *Window) stationImage(station radio.Station) *canvas.Image {
	img := canvas.NewImageFromResource(appIconResource)
	img.FillMode = canvas.ImageFillContain
	img.CornerRadius = stationArtworkCornerRadius
	iconSize := fyne.NewSquareSize(stationArtworkSize)
	img.SetMinSize(iconSize)
	img.Resize(iconSize)
	if station.ID != "" {
		if cached, ok := w.stationIcons[station.ID]; ok {
			applyStationIcon(img, cached)
			return img
		}
	}
	faviconURL := strings.TrimSpace(station.Favicon)
	homepageURL := strings.TrimSpace(station.Homepage)
	if faviconURL == "" && homepageURL == "" && (station.ID == "" || w.cacheStore == nil) {
		if !w.faviconMissingLogged[station.ID] {
			slog.Info("station icon unavailable; using fallback", "station", station.Name, "reason", "Radio Browser did not provide a favicon URL and station homepage is empty")
			w.faviconMissingLogged[station.ID] = true
		}
		return img
	}
	key := stationIconKey(station.ID, faviconURL, homepageURL)
	if cached, ok := w.faviconCache[key]; ok {
		applyStationIcon(img, cached)
		return img
	}
	if w.faviconFailed[key] {
		return img
	}
	if loading, ok := w.faviconLoading[key]; ok {
		w.faviconLoading[key] = append(loading, img)
		return img
	}
	w.faviconLoading[key] = []*canvas.Image{img}
	go w.fetchStationImage(station.ID, station.Name, key, faviconURL, homepageURL)
	return img
}

func stationArtworkFrame(image fyne.CanvasObject) *fyne.Container {
	return container.NewGridWrap(fyne.NewSquareSize(stationArtworkSize), image)
}

func stationIconKey(stationID, faviconURL, homepageURL string) string {
	if faviconURL == "" && homepageURL == "" && stationID != "" {
		return "station:" + stationID
	}
	return faviconURL + "\x00" + homepageURL
}

func (w *Window) fetchStationImage(stationID, stationName, key, faviconURL, homepageURL string) {
	ctx, cancel := context.WithTimeout(w.faviconContext, 8*time.Second)
	defer cancel()
	cacheKey := stationIconCacheKey(stationID, key)
	var cachedData []byte
	cacheResult, cacheErr := w.cacheStore.Get(cacheKey, stationIconCacheTTL, &cachedData)
	if cacheErr != nil {
		slog.Warn("could not read cached station icon", "station", stationName, "error", cacheErr)
	}
	if cacheResult.Found {
		cachedIcon, err := decodeStationIcon(cachedData)
		if err != nil {
			slog.Warn("cached station icon is invalid", "station", stationName, "error", err)
		} else {
			w.applyCachedStationIcon(stationID, key, cachedIcon, cacheResult.Fresh)
			if cacheResult.Fresh {
				return
			}
		}
	}
	if faviconURL == "" && homepageURL == "" {
		fyne.Do(func() {
			delete(w.faviconLoading, key)
			w.faviconFailed[key] = true
			if !w.faviconMissingLogged[stationID] {
				slog.Info("station icon unavailable; using fallback", "station", stationName, "reason", "Radio Browser did not provide a favicon URL and station homepage is empty")
				w.faviconMissingLogged[stationID] = true
			}
		})
		return
	}
	acquired := false
	select {
	case w.faviconSlots <- struct{}{}:
		acquired = true
		defer func() { <-w.faviconSlots }()
	case <-ctx.Done():
	}
	var icon stationIcon
	var err error
	if acquired {
		if homepageURL != "" {
			icon, err = downloadHomepageStationIcon(ctx, w.faviconClient, homepageURL)
			if err != nil {
				slog.Warn("station homepage favicon unavailable; trying Radio Browser favicon", "station", stationName, "homepage", homepageURL, "reason", err)
			}
		} else {
			err = fmt.Errorf("station homepage URL is unavailable")
		}
		if err != nil && faviconURL != "" && ctx.Err() == nil {
			icon, err = downloadStationIcon(ctx, w.faviconClient, faviconURL)
			if err == nil {
				slog.Info("station icon loaded from Radio Browser favicon", "station", stationName, "favicon", faviconURL)
			} else {
				slog.Warn("Radio Browser favicon unavailable; using fallback", "station", stationName, "favicon", faviconURL, "reason", err)
			}
		}
		if err == nil && homepageURL != "" {
			slog.Info("station icon loaded from homepage favicon", "station", stationName, "homepage", homepageURL)
		}
	} else {
		err = ctx.Err()
	}
	if err == nil {
		if data, encodeErr := encodeStationIcon(icon); encodeErr != nil {
			slog.Warn("could not encode station icon for cache", "station", stationName, "error", encodeErr)
		} else if len(data) > maxStationIconCacheSize {
			slog.Warn("station icon exceeds cache size limit", "station", stationName, "size_bytes", len(data))
		} else if cacheErr := w.cacheStore.Set(cacheKey, data); cacheErr != nil {
			slog.Warn("could not cache station icon", "station", stationName, "error", cacheErr)
		}
	}
	fyne.Do(func() {
		if w.faviconContext.Err() != nil {
			return
		}
		images := w.faviconLoading[key]
		delete(w.faviconLoading, key)
		if err != nil {
			w.faviconFailed[key] = true
			return
		}
		w.faviconCache[key] = icon
		if stationID != "" {
			w.stationIcons[stationID] = icon
		}
		for _, img := range images {
			applyStationIcon(img, icon)
		}
		w.updatePlayingStationIcon(stationID, icon)
	})
}

func stationIconCacheKey(stationID, sourceKey string) string {
	if stationID != "" {
		return "station-icon:v2:id:" + stationID
	}
	return "station-icon:v2:url:" + sourceKey
}

func (w *Window) applyCachedStationIcon(stationID, key string, icon stationIcon, fresh bool) {
	fyne.Do(func() {
		if w.faviconContext.Err() != nil {
			return
		}
		w.faviconCache[key] = icon
		if stationID != "" {
			w.stationIcons[stationID] = icon
		}
		for _, img := range w.faviconLoading[key] {
			applyStationIcon(img, icon)
		}
		w.updatePlayingStationIcon(stationID, icon)
		if fresh {
			delete(w.faviconLoading, key)
		}
	})
}

func (w *Window) updatePlayingStationIcon(stationID string, icon stationIcon) {
	if stationID == "" || w.playerArtID != stationID || w.playerArt == nil {
		return
	}
	applyStationIcon(w.playerArt, icon)
}

func encodeStationIcon(icon stationIcon) ([]byte, error) {
	if icon.resource != nil {
		return icon.resource.Content(), nil
	}
	if icon.image == nil {
		return nil, fmt.Errorf("station icon has no image data")
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, icon.image); err != nil {
		return nil, fmt.Errorf("encode station icon as PNG: %w", err)
	}
	return encoded.Bytes(), nil
}

func downloadHomepageStationIcon(ctx context.Context, client *http.Client, homepageURL string) (stationIcon, error) {
	homepage, err := url.Parse(homepageURL)
	if err != nil || homepage.Host == "" || (homepage.Scheme != "http" && homepage.Scheme != "https") {
		return stationIcon{}, fmt.Errorf("invalid station homepage URL")
	}
	data, _, err := favicon.Download(ctx, homepage, favicon.Options{Client: client})
	if err != nil {
		return stationIcon{}, fmt.Errorf("download homepage favicon: %w", err)
	}
	icon, err := decodeStationIcon(data)
	if err != nil {
		return stationIcon{}, fmt.Errorf("decode homepage favicon: %w", err)
	}
	return icon, nil
}

func downloadStationIcon(ctx context.Context, client *http.Client, faviconURL string) (stationIcon, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, faviconURL, nil)
	if err != nil {
		return stationIcon{}, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("User-Agent", version.UserAgent())
	response, err := client.Do(request)
	if err != nil {
		return stationIcon{}, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return stationIcon{}, fmt.Errorf("server returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return stationIcon{}, fmt.Errorf("read response: %w", err)
	}
	if len(data) > 2<<20 {
		return stationIcon{}, fmt.Errorf("image exceeds 2 MiB size limit")
	}
	icon, err := decodeStationIcon(data)
	if err != nil {
		contentType := response.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "not provided"
		}
		return stationIcon{}, fmt.Errorf("decode favicon response with Content-Type %q: %w", contentType, err)
	}
	return icon, nil
}

func decodeStationIcon(data []byte) (stationIcon, error) {
	if len(data) == 0 {
		return stationIcon{}, fmt.Errorf("response contained no image data")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		pixels := int64(config.Width) * int64(config.Height)
		if config.Width <= 0 || config.Height <= 0 || config.Width > maxStationIconDimension || config.Height > maxStationIconDimension || pixels > maxStationIconPixels {
			return stationIcon{}, fmt.Errorf("%s image dimensions %dx%d are outside the supported limit", format, config.Width, config.Height)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return stationIcon{}, fmt.Errorf("decode %s image: %w", format, err)
		}
		return stationIcon{image: decoded}, nil
	}
	if isSVG(data) {
		if err := validateSVG(data); err != nil {
			return stationIcon{}, fmt.Errorf("invalid SVG: %w", err)
		}
		return stationIcon{resource: fyne.NewStaticResource("station-favicon.svg", data)}, nil
	}
	return stationIcon{}, fmt.Errorf("unsupported or invalid image format: %w", err)
}

func isSVG(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return bytes.HasPrefix(trimmed, []byte("<svg")) || bytes.HasPrefix(trimmed, []byte("<?xml"))
}

func validateSVG(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	rootFound := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case xml.Directive:
			return fmt.Errorf("XML directives are not supported")
		case xml.StartElement:
			if !rootFound {
				if value.Name.Local != "svg" {
					return fmt.Errorf("root element is %q, want svg", value.Name.Local)
				}
				rootFound = true
			}
		}
	}
	if !rootFound {
		return fmt.Errorf("SVG has no root element")
	}
	return nil
}

func applyStationIcon(target *canvas.Image, icon stationIcon) {
	target.Image = icon.image
	target.Resource = icon.resource
	target.Refresh()
}

func (w *Window) discoveryRows(stations []radio.Station) *fyne.Container {
	rows := container.NewVBox()
	for _, station := range stations {
		s := station
		favorite := w.favoriteButton(s)
		rows.Add(w.stationRow(s, stationMeta(s, ""), favorite, func() { w.playStation(s) }, nil))
	}
	return rows
}

func (w *Window) refreshDiscoveryRows() {
	if w.remoteResults == nil || (w.page != pageDiscover && w.page != pageSearch) {
		return
	}
	w.visibleStationRows = nil
	w.remoteResults.Objects = w.discoveryRows(w.directoryStations).Objects
	w.remoteResults.Refresh()
	w.refreshPlayingRows()
}

func (w *Window) refreshPlayingRows() {
	for _, row := range w.visibleStationRows {
		row.SetHighlighted(w.stationIsActive(row.stationID))
	}
}

func (w *Window) isFavorite(id string) bool {
	for _, station := range w.settings.Stations {
		if station.ID == id {
			return true
		}
	}
	return false
}

func (w *Window) toggleFavorite(station radio.Station) {
	if w.isFavorite(station.ID) {
		rows := w.settings.Stations[:0]
		for _, existing := range w.settings.Stations {
			if existing.ID != station.ID {
				rows = append(rows, existing)
			}
		}
		w.settings.Stations = rows
	} else {
		w.settings.Stations = append(w.settings.Stations, station)
	}
	if err := w.store.SaveStations(w.settings.Stations); err != nil {
		dialog.ShowError(err, w.win)
	}
	if w.page == pageDiscover || w.page == pageSearch {
		w.refreshDiscoveryRows()
	} else {
		w.refreshPage()
	}
}

func (w *Window) scheduleDirectorySearch() {
	if w.searchTimer != nil {
		w.searchTimer.Stop()
	}
	query := radiobrowser.Query{}
	if w.searchName != nil {
		query.Name = w.searchName.Text
	}
	if w.searchCountry != nil {
		query.Country = w.searchCountryValue
		if query.Country == "Any country" {
			query.Country = ""
		}
	}
	if w.searchTag != nil {
		query.Tag = w.searchTag.Selected
		if query.Tag == "Any genre" {
			query.Tag = ""
		}
	}
	if w.searchCodec != nil {
		query.Codec = w.searchCodec.Selected
		if query.Codec == "Any codec" {
			query.Codec = ""
		}
	}
	w.searchTimer = time.AfterFunc(350*time.Millisecond, func() {
		fyne.Do(func() {
			if w.page != pageSearch {
				return
			}
			if query.Name == "" && query.Country == "" && query.Tag == "" && query.Codec == "" {
				w.setDirectoryStatus("Enter a search or choose a filter.")
				w.remoteResults.Objects = nil
				w.remoteResults.Refresh()
				w.resultCount.SetText("")
				return
			}
			if w.directory != nil {
				w.fetchDirectory(query, false)
			}
		})
	})
}

func (w *Window) countryLabel() string {
	country := w.settings.DiscoveryCountry
	switch {
	case country == "", country == "*":
		return "All countries"
	case strings.HasPrefix(country, "@"):
		code := strings.TrimPrefix(country, "@")
		if name := w.countryNameForCode(code); name != "" {
			return name
		}
		return code
	default:
		return country
	}
}

func (w *Window) popularHeading() string {
	if w.settings.DiscoveryCountry == "" || w.settings.DiscoveryCountry == "*" {
		return "Popular stations · All countries"
	}
	return "Popular in " + w.countryLabel()
}

func (w *Window) countryNameForCode(code string) string {
	for _, country := range w.countries {
		if strings.EqualFold(country.Code, code) {
			return country.Name
		}
	}
	return ""
}

func (w *Window) loadCountries() {
	if w.directory == nil {
		w.setCountryLoadFailure()
		return
	}
	ctx, cancel := context.WithTimeout(w.faviconContext, 12*time.Second)
	go func() {
		defer cancel()
		var cached []radiobrowser.Country
		cacheResult, cacheErr := w.cacheStore.Get("radio-browser:countries:v2", radioBrowserCacheTTL, &cached)
		if cacheErr != nil {
			slog.Warn("could not read cached Radio Browser countries", "error", cacheErr)
		}
		if cacheResult.Found {
			w.setCountryOptions(cached)
		}
		if cacheResult.Fresh {
			return
		}
		countries, err := w.directory.Countries(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !cacheResult.Found {
				w.setCountryLoadFailure()
				return
			}
			slog.Warn("Radio Browser country refresh failed; using cached list", "error", err)
			return
		}
		if cacheErr := w.cacheStore.Set("radio-browser:countries:v2", countries); cacheErr != nil {
			slog.Warn("could not cache Radio Browser countries", "error", cacheErr)
		}
		w.setCountryOptions(countries)
	}()
}

func (w *Window) setCountryLoadFailure() {
	fyne.Do(func() {
		if w.faviconContext.Err() != nil {
			return
		}
		if w.regionStatus != nil {
			message := "The country list is unavailable. You can continue with all countries and choose a region later."
			if w.regionSuggestionCode != "" {
				message = "The country list is unavailable. You can continue with the country from your device settings and change it later."
			}
			w.regionStatus.SetText(message)
		}
		if w.regionContinue != nil {
			w.regionContinue.Enable()
		}
	})
}

func (w *Window) setCountryOptions(countries []radiobrowser.Country) {
	sort.Slice(countries, func(i, j int) bool { return countries[i].Name < countries[j].Name })
	fyne.Do(func() {
		if w.faviconContext.Err() != nil {
			return
		}
		w.countries = countries
		options := []string{"Any country"}
		for _, country := range countries {
			if country.Name != "" && !contains(options, country.Name) {
				options = append(options, country.Name)
			}
		}
		w.countryOptions = options
		if w.regionStatus != nil {
			w.regionStatus.Hide()
		}
		if w.regionContinue != nil {
			w.regionContinue.Enable()
		}
		if w.regionSelect != nil {
			selected := w.regionSelect.Text
			regionOptions := w.regionOptions()
			if !w.regionSelect.focused {
				switch {
				case strings.EqualFold(selected, "Any country"):
					w.setRegionText("Any country")
				case selected == "" || strings.EqualFold(selected, w.regionSuggestionCode):
					w.setRegionText(w.initialRegionOption(regionOptions))
				case contains(regionOptions, selected):
					w.setRegionText(selected)
				}
				w.regionPreviousChoice = w.regionSelect.Text
			}
			if w.regionOptionsVisible {
				w.showRegionOptions(w.regionSelect.Text)
			}
		}
		if w.discoveryHeading != nil {
			w.discoveryHeading.SetText(w.popularHeading())
		}
		if w.searchCountry != nil {
			if !w.searchCountry.focused {
				if contains(options, w.searchCountryValue) {
					w.setSearchCountryText(w.searchCountryValue)
				} else {
					w.searchCountryValue = ""
					w.setSearchCountryText("")
				}
			}
			if w.searchCountryOptionsVisible {
				w.showSearchCountryOptions(w.searchCountry.Text)
			}
		}
	})
}

func (w *Window) loadPopular() {
	query := radiobrowser.Query{Limit: 20}
	country := w.settings.DiscoveryCountry
	switch {
	case country == "", country == "*":
		w.fetchDirectory(query, true)
	case strings.HasPrefix(country, "@"):
		query.CountryCode = strings.TrimPrefix(country, "@")
		w.fetchDirectory(query, false)
	default:
		query.Country = country
		query.CountryExact = true
		w.fetchDirectory(query, false)
	}
}

func systemLocaleCountryCode() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		locale := strings.TrimSpace(os.Getenv(key))
		if locale == "" {
			continue
		}
		if prefix, _, found := strings.Cut(locale, "."); found {
			locale = prefix
		}
		if prefix, _, found := strings.Cut(locale, "@"); found {
			locale = prefix
		}
		parts := strings.FieldsFunc(locale, func(r rune) bool { return r == '_' || r == '-' })
		if len(parts) < 2 || len(parts[1]) != 2 {
			continue
		}
		code := strings.ToUpper(parts[1])
		if code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			return code
		}
	}
	return ""
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func (w *Window) cancelDirectoryRequest() {
	w.directoryMu.Lock()
	if w.directoryCancel != nil {
		w.directoryCancel()
		w.directoryCancel = nil
	}
	w.directorySeq++
	w.directoryMu.Unlock()
}

func (w *Window) fetchDirectory(query radiobrowser.Query, popular bool) {
	if w.directory == nil {
		return
	}
	w.directoryMu.Lock()
	if w.directoryCancel != nil {
		w.directoryCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.directoryCancel = cancel
	w.directorySeq++
	seq := w.directorySeq
	w.directoryMu.Unlock()
	fyne.Do(func() {
		w.setDirectoryStatus("Searching Radio Browser…")
		if w.remoteResults != nil {
			w.remoteResults.Objects = nil
			w.remoteResults.Refresh()
		}
	})
	go func() {
		key := directoryCacheKey(query, popular)
		var cached []radio.Station
		cacheResult, cacheErr := w.cacheStore.Get(key, radioBrowserCacheTTL, &cached)
		if cacheErr != nil {
			slog.Warn("could not read cached Radio Browser stations", "error", cacheErr)
		}
		retry := func() { w.fetchDirectory(query, popular) }
		if cacheResult.Found {
			status := ""
			if !cacheResult.Fresh {
				status = "Refreshing saved stations…"
			}
			w.showDirectoryResults(seq, cached, nil, retry, status)
			if cacheResult.Fresh {
				return
			}
		}
		var stations []radio.Station
		var err error
		if popular {
			stations, err = w.directory.Popular(ctx, query.Limit)
		} else {
			stations, err = w.directory.Search(ctx, query)
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			if cacheErr := w.cacheStore.Set(key, stations); cacheErr != nil {
				slog.Warn("could not cache Radio Browser stations", "error", cacheErr)
			}
			w.showDirectoryResults(seq, stations, nil, retry, "")
			return
		}
		if cacheResult.Found {
			slog.Warn("Radio Browser station refresh failed; using cached results", "error", err)
			w.showDirectoryResults(seq, cached, nil, retry, "Offline · showing saved stations")
			return
		}
		w.showDirectoryResults(seq, nil, err, retry, "")
	}()
}

func directoryCacheKey(query radiobrowser.Query, popular bool) string {
	key, _ := json.Marshal(struct {
		Popular bool
		Query   radiobrowser.Query
	}{Popular: popular, Query: query})
	return "radio-browser:stations:v1:" + string(key)
}

func (w *Window) showDirectoryResults(seq uint64, stations []radio.Station, err error, retry func(), status string) {
	fyne.Do(func() {
		w.directoryMu.Lock()
		current := seq == w.directorySeq
		w.directoryMu.Unlock()
		if !current || (w.page != pageDiscover && w.page != pageSearch) {
			return
		}
		w.setDirectoryStatus(status)
		if err != nil {
			retryButton := widget.NewButton("Retry", retry)
			w.remoteResults.Objects = []fyne.CanvasObject{w.emptyState(theme.WarningIcon(), "Could not load stations", "Check your connection and try again."), retryButton}
			w.remoteResults.Refresh()
			if w.resultCount != nil {
				w.resultCount.SetText("")
			}
			return
		}
		w.directoryStations = stations
		if len(stations) == 0 {
			w.remoteResults.Objects = []fyne.CanvasObject{w.emptyState(theme.SearchIcon(), "No stations found", "Try another search or adjust the filters.")}
		} else {
			w.remoteResults.Objects = w.discoveryRows(stations).Objects
		}
		w.remoteResults.Refresh()
		if w.resultCount != nil {
			label := fmt.Sprintf("%d stations", len(stations))
			if len(stations) == 1 {
				label = "1 station"
			}
			w.resultCount.SetText(label)
		}
	})
}

func stationHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "Internet radio"
	}
	return u.Hostname()
}

func (w *Window) playStation(station radio.Station) {
	w.selected = station
	w.settings.Selected = station.ID
	w.recent = append([]recentStation{{Station: station, PlayedAt: time.Now()}}, w.withoutRecent(station.ID)...)
	if len(w.recent) > 20 {
		w.recent = w.recent[:20]
	}
	if err := w.store.SaveRecent(w.recent); err != nil {
		slog.Warn("could not save recent stations", "error", err)
	}
	_ = w.store.SaveSettings(w.settings)
	w.updatePlayerBar()
	w.artist.SetText("Connecting…")
	w.setPlayControls(theme.MediaStopIcon())
	if w.page == pageFavorites || w.page == pageRecent {
		w.refreshPage()
	}
	w.updateTray()
	w.player.Play(station)
}

func (w *Window) withoutRecent(id string) []recentStation {
	result := make([]recentStation, 0, len(w.recent))
	for _, item := range w.recent {
		if item.Station.ID != id {
			result = append(result, item)
		}
	}
	return result
}

func (w *Window) updatePlayerBar() {
	if w.playerArtwork != nil && w.playerArtID != w.selected.ID {
		var art *canvas.Image
		if w.selected.ID == "" {
			art = canvas.NewImageFromResource(appIconResource)
			art.FillMode = canvas.ImageFillContain
			art.CornerRadius = stationArtworkCornerRadius
			art.SetMinSize(fyne.NewSquareSize(stationArtworkSize))
			art.Resize(fyne.NewSquareSize(stationArtworkSize))
		} else {
			art = w.stationImage(w.selected)
		}
		w.playerArt = art
		w.playerArtwork.Objects = []fyne.CanvasObject{art}
		w.playerArtwork.Refresh()
		w.playerArtID = w.selected.ID
	}
	if w.selected.ID == "" {
		w.title.SetText("Choose a station")
		w.artist.SetText("Not playing")
		return
	}
	w.title.SetText(w.selected.Name)
	if w.player.State() == radio.StateStopped {
		w.artist.SetText("Not playing")
	}
	w.updateVolumeControls()
}

func (w *Window) toggleExpandedPlayer() {
	w.playerExpanded = !w.playerExpanded
	if w.playerExpanded {
		w.expandedPlayer.Show()
	} else {
		w.expandedPlayer.Hide()
	}
	w.expandedPlayer.Refresh()
	w.win.Content().Refresh()
}

func (w *Window) setPlayControls(icon fyne.Resource) {
	w.play.SetIcon(icon)
}

func (w *Window) updateTray() {
	if w.tray == nil {
		return
	}
	station := "No station selected"
	if w.selected.ID != "" {
		station = w.selected.Name
	}
	current := "Not playing"
	if w.artist.Text != "" && w.artist.Text != "Not playing" {
		current = w.artist.Text
	}
	playLabel := "Play"
	if state := w.player.State(); state != radio.StateStopped && state != radio.StateError {
		playLabel = "Stop"
	}
	stationItem := fyne.NewMenuItem(station, nil)
	stationItem.Disabled = true
	trackItem := fyne.NewMenuItem(current, nil)
	trackItem.Disabled = true
	w.tray.SetSystemTrayMenu(fyne.NewMenu("FyneTune", stationItem, trackItem, fyne.NewMenuItemSeparator(), fyne.NewMenuItem(playLabel, w.togglePlay), fyne.NewMenuItem("Show Window", func() { w.win.Show() }), fyne.NewMenuItem("Quit", func() { _ = w.Close(); w.app.Quit() })))
}

func (w *Window) togglePlay() {
	if w.player.State() == radio.StateStopped || w.player.State() == radio.StateError {
		if w.selected.ID != "" {
			w.playStation(w.selected)
		}
	} else {
		w.player.Stop()
	}
}

func (w *Window) events() {
	for {
		select {
		case <-w.done:
			return
		case e, ok := <-w.player.Events():
			if !ok {
				return
			}
			ev := e
			fyne.Do(func() {
				if ev.State != radio.StateStopped && ev.Station.ID != "" && ev.Station.ID != w.selected.ID {
					return
				}
				switch ev.State {
				case radio.StateStopped:
					w.artist.SetText("Not playing")
					w.setPlayControls(theme.MediaPlayIcon())
					w.updatePlayerBar()
				case radio.StateConnecting:
					w.title.SetText(ev.Station.Name)
					w.artist.SetText("Connecting…")
					w.setPlayControls(theme.MediaStopIcon())
				case radio.StateBuffering:
					w.artist.SetText("Buffering…")
				case radio.StatePlaying:
					w.title.SetText(ev.Station.Name)
					w.setPlayControls(theme.MediaStopIcon())
					if ev.Metadata.RawTitle != "" {
						w.artist.SetText(ev.Metadata.String())
					} else {
						w.artist.SetText("Now playing")
					}
				case radio.StateReconnecting:
					if ev.Err != nil {
						w.artist.SetText("Connection interrupted · retrying")
					} else {
						w.artist.SetText(fmt.Sprintf("Reconnecting · attempt %d", ev.Attempt))
					}
					w.setPlayControls(theme.MediaStopIcon())
				case radio.StateError:
					message := "Playback failed"
					if ev.Err != nil {
						message = ev.Err.Error()
					}
					w.setPlayControls(theme.MediaPlayIcon())
					w.title.SetText(ev.Station.Name)
					w.artist.SetText(message)
				}
				w.refreshPlayingRows()
				if ev.State != radio.StatePlaying || ev.Metadata.RawTitle == "" {
					w.refreshDiscoveryRows()
				}
				w.updateTray()
			})
		}
	}
}

func (w *Window) stationDialog(old *radio.Station) {
	name, urlText := "", ""
	title := "Add radio station"
	if old != nil {
		name, urlText, title = old.Name, old.URL, "Edit radio station"
	}
	n := widget.NewEntry()
	n.SetText(name)
	u := widget.NewEntry()
	u.SetText(urlText)
	form := widget.NewForm(widget.NewFormItem("Station name", n), widget.NewFormItem("Stream URL", u))
	dialog.ShowCustomConfirm(title, "Save", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		s := radio.Station{}
		if old != nil {
			s = *old
			if strings.TrimSpace(u.Text) != old.URL {
				s.ResolvedURL = ""
			}
		}
		s.Name = strings.TrimSpace(n.Text)
		s.URL = strings.TrimSpace(u.Text)
		if err := radio.ValidateStation(s); err != nil {
			dialog.ShowError(err, w.win)
			return
		}
		if old != nil {
			s.ID = old.ID
			for i, v := range w.settings.Stations {
				if v.ID == old.ID {
					w.settings.Stations[i] = s
				}
			}
			if w.selected.ID == s.ID {
				w.selected = s
				w.updatePlayerBar()
				w.updateTray()
			}
		} else {
			id := make([]byte, 12)
			if _, err := rand.Read(id); err != nil {
				dialog.ShowError(err, w.win)
				return
			}
			s.ID = hex.EncodeToString(id)
			w.settings.Stations = append(w.settings.Stations, s)
		}
		if err := w.store.SaveStations(w.settings.Stations); err != nil {
			dialog.ShowError(err, w.win)
		}
		w.refreshPage()
	}, w.win)
}

func (w *Window) stationMenu(s radio.Station) {
	play := widget.NewButton("Play", func() { w.playStation(s) })
	edit := widget.NewButton("Edit", func() { w.stationDialog(&s) })
	remove := widget.NewButton("Remove", func() {
		dialog.ShowConfirm("Remove station", "Remove "+s.Name+"?", func(ok bool) {
			if !ok {
				return
			}
			if w.selected.ID == s.ID {
				w.player.Stop()
				w.selected = radio.Station{}
				w.settings.Selected = ""
			}
			rows := w.settings.Stations[:0]
			for _, row := range w.settings.Stations {
				if row.ID != s.ID {
					rows = append(rows, row)
				}
			}
			w.settings.Stations = rows
			_ = w.store.SaveStations(rows)
			_ = w.store.SaveSettings(w.settings)
			w.refreshPage()
			w.updatePlayerBar()
		}, w.win)
	})
	d := dialog.NewCustom(s.Name, "Close", container.NewVBox(play, edit, remove), w.win)
	d.Show()
}

func (w *Window) preferencesPage() *fyne.Container {
	auto := preferenceCheckRow("Play last station on startup", w.settings.AutoPlay, func(v bool) {
		w.settings.AutoPlay = v
		_ = w.store.SaveSettings(w.settings)
	})
	rec := preferenceCheckRow("Reconnect automatically", w.settings.Reconnect, func(v bool) {
		w.settings.Reconnect = v
		w.player.SetReconnect(v)
		_ = w.store.SaveSettings(w.settings)
	})
	min := preferenceCheckRow("Start minimized", w.settings.StartMinimized, func(v bool) {
		w.settings.StartMinimized = v
		_ = w.store.SaveSettings(w.settings)
	})

	playbackHeading := widget.NewLabelWithStyle("Playback", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	startupHeading := widget.NewLabelWithStyle("Startup", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	aboutHeading := widget.NewLabelWithStyle("About FyneTune", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	startupPreferences := []fyne.CanvasObject{auto}
	if !w.app.Driver().Device().IsMobile() {
		startupPreferences = append(startupPreferences, min)
	}
	info := version.Current()
	about := container.NewVBox(aboutHeading, widget.NewLabel("Version "+info.Version))
	if info.Commit != "" {
		about.Add(widget.NewLabel("Commit " + version.ShortCommit(info.Commit)))
	}
	websiteURL, _ := url.Parse("https://fynetune.lucor.dev")
	website := widget.NewHyperlink("fynetune.lucor.dev", websiteURL)
	about.Add(website)
	return container.NewVBox(
		playbackHeading,
		rec,
		widget.NewSeparator(),
		startupHeading,
		container.NewVBox(startupPreferences...),
		widget.NewSeparator(),
		about,
	)
}

func preferenceCheckRow(label string, checked bool, onChanged func(bool)) fyne.CanvasObject {
	check := widget.NewCheck("", onChanged)
	check.SetChecked(checked)
	checkTouchArea := container.NewCenter(container.NewGridWrap(fyne.NewSize(48, 48), check))
	text := widget.NewLabel(label)
	text.Wrapping = fyne.TextWrapWord
	return container.NewBorder(nil, nil, nil, checkTouchArea, text)
}

func (w *Window) setAboutMenu() {
	if runtime.GOOS == "android" {
		return
	}
	about := fyne.NewMenuItem("About", w.aboutDialog)
	menuName := "Help"
	if runtime.GOOS == "darwin" {
		menuName = "FyneTune"
	}
	w.win.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu(menuName, about)))
}

func (w *Window) aboutDialog() {
	logo := canvas.NewImageFromResource(logoResource)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(180, 50))
	websiteURL, _ := url.Parse("https://fynetune.lucor.dev")
	website := widget.NewHyperlink("fynetune.lucor.dev", websiteURL)
	website.Alignment = fyne.TextAlignCenter
	info := version.Current()
	var details []fyne.CanvasObject
	if info.Version != "" {
		versionLabel := widget.NewLabel("Version " + info.Version)
		versionLabel.Alignment = fyne.TextAlignCenter
		details = append(details, versionLabel)
	}
	if info.Commit != "" {
		commitLabel := widget.NewLabel("Commit " + version.ShortCommit(info.Commit))
		commitLabel.Alignment = fyne.TextAlignCenter
		details = append(details, commitLabel)
	}
	content := container.NewVBox(container.NewCenter(logo), container.NewVBox(details...), container.NewCenter(website))
	dialog.ShowCustom("About FyneTune", "Close", container.NewPadded(content), w.win)
}

func (w *Window) Close() error {
	w.closeOnce.Do(func() {
		if w.faviconCancel != nil {
			w.faviconCancel()
		}
		w.cancelDirectoryRequest()
		if w.searchTimer != nil {
			w.searchTimer.Stop()
		}
		close(w.done)
		w.closeErr = w.player.Close()
		<-w.eventsDone
	})
	return w.closeErr
}

func (w *Window) Run() {
	if w.settings.StartMinimized && w.hasTray {
		w.win.Hide()
	} else {
		w.win.Show()
	}
	w.app.Run()
}
