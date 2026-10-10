// Package ui contains the FyneTune application interface.
package ui

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/hashicorp/golang-lru/v2"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
	"go.lucor.dev/fynetune/internal/ui/navigation"
	"go.lucor.dev/fynetune/internal/ui/widgets"
)

// Window owns the application dependencies and switches between its two views.
type Window struct {
	app                  fyne.App
	window               fyne.Window
	theme                *brandTheme
	sectionHeader        *widget.Label
	settingsCountryFlag  *canvas.Text
	settingsCountryName  *widget.Label
	settingsVersionBadge *widgets.Badge
	themeIcons           []themeIcon
	bottomNavigation     *navigation.Navigation
	radioPlayer          radio.Player
	store                *storage.Store
	settings             storage.Settings
	directory            radiobrowser.Directory
	iconCache            *lru.Cache[string, favicon.Icon]
	appContent           fyne.CanvasObject
	onboarding           *onboarding
	home                 *homeSection
	favorites            *favoritesSection
	recent               []storage.RecentStation
	recentSection        *recentSection
	playerBar            *playerWidget
	tray                 *systemTray
	volumeMuted          bool
	volumeBeforeMute     float64
	selectedStation      radio.Station
	initialStations      []radio.Station
	done                 chan struct{}
	eventsDone           chan struct{}
	closeOnce            sync.Once
	closeErr             error
}

type themeIcon struct {
	widget   *widget.Icon
	resource func() fyne.Resource
}

// New creates the window and displays onboarding when no discovery country is saved.
func New(a fyne.App, radioPlayer radio.Player, directory radiobrowser.Directory) *Window {
	systemTheme := a.Settings().Theme()
	store := storage.New(a)
	settings, err := store.Load()
	if err != nil {
		slog.Warn("could not load settings", "error", err)
	}
	window := a.NewWindow("FyneTune")
	appTheme := &brandTheme{Theme: systemTheme, appearance: settings.Appearance}
	a.Settings().SetTheme(appTheme)
	slog.Debug("UI theme selected",
		"appearance", settings.Appearance,
		"variant", themeVariantName(a.Settings().ThemeVariant()),
		"fyne_theme", fmt.Sprintf("%T", systemTheme),
		"section_title_color", theme.Color(sectionTitleColorName),
	)
	lastVariant := a.Settings().ThemeVariant()
	recent, err := store.LoadRecent()
	if err != nil {
		slog.Warn("could not load recent stations", "error", err)
		recent = nil
	}
	w := &Window{
		app:              a,
		window:           window,
		theme:            appTheme,
		radioPlayer:      radioPlayer,
		store:            store,
		settings:         settings,
		volumeBeforeMute: settings.Volume,
		recent:           recent,
		directory:        directory,
		done:             make(chan struct{}),
		eventsDone:       make(chan struct{}),
	}
	radioPlayer.SetVolume(settings.Volume)
	radioPlayer.SetReconnect(settings.Reconnect)
	w.appContent = newApp(w)
	a.Settings().AddListener(func(settings fyne.Settings) {
		variant := settings.ThemeVariant()
		if variant == lastVariant {
			return
		}
		lastVariant = variant
		w.refreshIconTheme()
		sectionTitleColor := settings.Theme().Color(sectionTitleColorName, variant)
		if w.sectionHeader != nil {
			w.sectionHeader.Refresh()
		}
		if w.favorites != nil {
			w.favorites.refreshTheme()
		}
		w.refreshSettingsTheme()
		slog.Debug("Fyne theme variant changed",
			"variant", themeVariantName(variant),
			"appearance", appTheme.appearance,
			"section_title_color", sectionTitleColor,
		)
	})
	if desktopApp, ok := a.(desktop.App); ok {
		w.tray = newSystemTray(desktopApp, window, homeBrandIcon, w.togglePlayback, func() {
			_ = w.Close()
			a.Quit()
		})
		window.SetCloseIntercept(func() { window.Hide() })
	}

	initialContent := w.appContent
	if settings.DiscoveryCountry == "" {
		w.onboarding = newOnboarding(w)
		initialContent = w.onboarding.welcomeScreen()
	}
	window.Resize(fyne.NewSize(480, 800))
	w.setContent(initialContent)
	if w.onboarding != nil {
		w.onboarding.loadCountries()
	} else if w.home != nil {
		w.home.loadPopular()
	}
	w.backfillSavedCountryCodes()
	go w.events()
	w.restorePlayback()
	return w
}

// setAppearance changes the app theme while preserving the system theme as the default.
func (w *Window) setAppearance(appearance string) {
	switch appearance {
	case appearanceLight, appearanceDark:
	default:
		appearance = appearanceSystem
	}
	w.settings.Appearance = appearance
	w.theme.appearance = appearance
	w.app.Settings().SetTheme(w.theme)
	w.refreshIconTheme()
	if w.sectionHeader != nil {
		w.sectionHeader.Refresh()
	}
	if w.favorites != nil {
		w.favorites.refreshTheme()
	}
	w.refreshSettingsTheme()
	err := w.saveSettings()
	slog.Debug("UI appearance setting changed",
		"appearance", appearance,
		"variant", themeVariantName(w.app.Settings().ThemeVariant()),
		"section_title_color", theme.Color(sectionTitleColorName),
		"persisted", err == nil,
	)
}

// newThemeIcon creates an icon widget that can be refreshed when the app theme changes.
func (w *Window) newThemeIcon(resource func() fyne.Resource) *widget.Icon {
	icon := widget.NewIcon(resource())
	w.themeIcons = append(w.themeIcons, themeIcon{widget: icon, resource: resource})
	return icon
}

// refreshIconTheme updates embedded PNG icons after the Fyne theme variant changes.
func (w *Window) refreshIconTheme() {
	for _, icon := range w.themeIcons {
		icon.widget.SetResource(icon.resource())
	}
	if w.bottomNavigation != nil {
		w.bottomNavigation.SetItemIcons(1, icons.Favorites(), icons.FavoritesActive())
		w.bottomNavigation.SetItemIcons(2, icons.Recent(), icons.RecentActive())
		w.bottomNavigation.SetItemIcons(3, icons.Discover(), icons.DiscoverActive())
		w.bottomNavigation.SetItemIcons(4, icons.Settings(), icons.SettingsActive())
	}
	if w.playerBar != nil {
		w.playerBar.refreshIconTheme()
	}
	if w.home != nil {
		w.home.rows.refreshIconTheme()
	}
	if w.favorites != nil {
		w.favorites.rows.refreshIconTheme()
	}
	if w.recentSection != nil {
		w.recentSection.rows.refreshIconTheme()
	}
}

// changeCountry opens the searchable country list from Settings.
func (w *Window) changeCountry() {
	if w.directory == nil {
		return
	}
	o := newOnboarding(w)
	o.fromSettings = true
	o.countryActivity = widget.NewActivity()
	o.countryActivity.Start()
	w.onboarding = o
	w.setContent(container.NewCenter(container.NewVBox(
		o.countryActivity,
		widget.NewLabel("Loading countries…"),
	)))
	o.loadCountries()
}

// restorePlayback restores the saved station and applies the startup playback preference.
func (w *Window) restorePlayback() {
	for _, station := range w.settings.Stations {
		if station.ID == w.settings.Selected {
			w.selectedStation = station
			break
		}
	}
	if w.selectedStation.ID == "" && w.settings.Selected != "" {
		for _, item := range w.recent {
			if item.Station.ID == w.settings.Selected {
				w.selectedStation = item.Station
				break
			}
		}
	}
	if w.selectedStation.ID == "" && len(w.settings.Stations) > 0 {
		w.selectedStation = w.settings.Stations[0]
	}
	w.playerBar.setPlayback(w.selectedStation, "Not playing", false)
	w.updateTray()
	if w.settings.AutoPlay && w.selectedStation.ID != "" {
		w.selectStation(w.selectedStation)
	}
}

// Run shows the window and starts the Fyne event loop.
func (w *Window) Run() {
	if w.settings.StartMinimized && w.tray != nil {
		w.window.Hide()
		w.app.Run()
	} else {
		w.window.ShowAndRun()
	}
	_ = w.Close()
}

// Close stops playback and waits for the player event listener to exit.
func (w *Window) Close() error {
	w.closeOnce.Do(func() {
		close(w.done)
		w.closeErr = w.radioPlayer.Close()
		<-w.eventsDone
	})
	return w.closeErr
}

// showOnboarding replaces the app view with a fresh country setup flow.
func (w *Window) showOnboarding() {
	w.onboarding = newOnboarding(w)
	w.setContent(w.onboarding.welcomeScreen())
	w.onboarding.loadCountries()
}

// showApp returns to the app view after onboarding completes.
func (w *Window) showApp() {
	w.onboarding = nil
	w.setContent(w.appContent)
	if w.home == nil {
		return
	}
	if len(w.initialStations) > 0 {
		w.home.setStations(w.initialStations)
	} else {
		w.home.loadPopular()
	}
}

// setContent installs the active top-level view in the window.
func (w *Window) setContent(content fyne.CanvasObject) {
	w.window.SetContent(content)
}

// isFavorite reports whether a station is saved in the user's favorites.
func (w *Window) isFavorite(id string) bool {
	for _, station := range w.settings.Stations {
		if station.ID == id {
			return true
		}
	}
	return false
}

// selectStation saves and starts the station selected from the list.
func (w *Window) selectStation(station radio.Station) {
	w.selectedStation = station
	w.settings.Selected = station.ID
	w.recent = recordRecent(w.recent, storage.RecentStation{Station: station, PlayedAt: time.Now()})
	if err := w.store.SaveRecent(w.recent); err != nil {
		slog.Warn("could not save recent stations", "error", err)
	}
	if w.recentSection != nil {
		w.recentSection.refresh()
	}
	if err := w.store.SaveSettings(w.settings); err != nil {
		dialog.ShowError(err, w.window)
	}
	w.playerBar.setPlayback(station, "Connecting…", true)
	w.radioPlayer.Play(station)
	w.updateTray()
}

// togglePlayback stops playback or restarts the selected station.
func (w *Window) togglePlayback() {
	switch w.radioPlayer.State() {
	case radio.StateConnecting, radio.StateBuffering, radio.StatePlaying, radio.StateReconnecting:
		w.radioPlayer.Stop()
	default:
		if w.selectedStation.ID != "" {
			w.playerBar.setPlayback(w.selectedStation, "Connecting…", true)
			w.radioPlayer.Play(w.selectedStation)
		}
	}
	w.updateTray()
}

// updateTray mirrors the current station and playback state in the system tray.
func (w *Window) updateTray() {
	if w.tray == nil {
		return
	}
	status := "Not playing"
	if w.playerBar != nil && w.playerBar.track != nil {
		status = w.playerBar.track.Text
	}
	w.tray.update(w.selectedStation, status, w.radioPlayer.State())
}

// setVolume updates the saved volume and applies it to playback.
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
	if err := w.store.SaveSettings(w.settings); err != nil {
		slog.Warn("could not save volume setting", "error", err)
	}
	w.applyVolume()
}

// toggleMute silences playback while keeping the chosen volume for unmuting.
func (w *Window) toggleMute() {
	if w.volumeMuted || w.settings.Volume == 0 {
		w.volumeMuted = false
		if w.settings.Volume == 0 {
			w.settings.Volume = w.volumeBeforeMute
			if w.settings.Volume <= 0 {
				w.settings.Volume = 0.5
			}
		}
	} else {
		w.volumeBeforeMute = w.settings.Volume
		w.volumeMuted = true
	}
	if err := w.store.SaveSettings(w.settings); err != nil {
		slog.Warn("could not save volume setting", "error", err)
	}
	w.applyVolume()
}

// applyVolume sends the effective level to the player and refreshes the controls.
func (w *Window) applyVolume() {
	volume := w.settings.Volume
	if w.volumeMuted {
		volume = 0
	}
	w.radioPlayer.SetVolume(volume)
	if w.playerBar != nil {
		w.playerBar.setVolumeState(w.settings.Volume, w.volumeMuted)
	}
}

// events reflects playback and ICY metadata changes in the persistent player.
func (w *Window) events() {
	defer close(w.eventsDone)
	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.radioPlayer.Events():
			if !ok {
				return
			}
			current := event
			fyne.Do(func() { w.updatePlayer(current) })
		}
	}
}

// updatePlayer applies one player event when it belongs to the selected station.
func (w *Window) updatePlayer(event radio.Event) {
	if event.Station.ID != "" && event.Station.ID != w.selectedStation.ID {
		return
	}
	station := w.selectedStation
	status := "Not playing"
	playing := false
	switch event.State {
	case radio.StateConnecting:
		station = event.Station
		status, playing = "Connecting…", true
	case radio.StateBuffering:
		station = event.Station
		status, playing = "Buffering…", true
	case radio.StatePlaying:
		station = event.Station
		status = event.Metadata.String()
		if status == "" {
			status = "Now playing"
		}
		playing = true
	case radio.StateReconnecting:
		station = event.Station
		status, playing = "Connection interrupted · retrying", true
	case radio.StateError:
		station = event.Station
		if event.Err != nil {
			status = event.Err.Error()
		} else {
			status = "Playback failed"
		}
	case radio.StateStopped:
		if w.radioPlayer.State() != radio.StateStopped {
			return
		}
	}
	w.playerBar.setPlayback(station, status, playing)
	w.updateTray()
}

// toggleFavorite saves or removes a station and refreshes its visible row.
func (w *Window) toggleFavorite(station radio.Station) {
	if w.isFavorite(station.ID) {
		favorites := w.settings.Stations[:0]
		for _, existing := range w.settings.Stations {
			if existing.ID != station.ID {
				favorites = append(favorites, existing)
			}
		}
		w.settings.Stations = favorites
	} else {
		w.settings.Stations = append(w.settings.Stations, station)
	}
	if err := w.store.SaveStations(w.settings.Stations); err != nil {
		dialog.ShowError(err, w.window)
	}
	if w.home != nil {
		w.home.rows.list.Refresh()
	}
	if w.favorites != nil {
		w.favorites.refresh()
	}
	if w.recentSection != nil {
		w.recentSection.refresh()
	}
}

// backfillSavedCountryCodes updates stations saved before ISO codes were stored.
func (w *Window) backfillSavedCountryCodes() {
	if w.directory == nil {
		return
	}
	needsBackfill := false
	for _, station := range w.settings.Stations {
		needsBackfill = needsBackfill || station.CountryCode == "" && countryNameKey(station.Country) != ""
	}
	for _, item := range w.recent {
		needsBackfill = needsBackfill || item.Station.CountryCode == "" && countryNameKey(item.Station.Country) != ""
	}
	if !needsBackfill {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		countries, err := w.directory.Countries(ctx)
		if err != nil {
			slog.Warn("could not update saved station country codes", "error", err)
			return
		}
		codes := countryCodesByName(countries)
		fyne.Do(func() {
			favoritesChanged := false
			for i, station := range w.settings.Stations {
				if updated, changed := withCountryCode(station, codes); changed {
					w.settings.Stations[i] = updated
					favoritesChanged = true
				}
			}
			if favoritesChanged {
				if err := w.store.SaveStations(w.settings.Stations); err != nil {
					slog.Warn("could not save station country codes", "error", err)
				}
				if w.favorites != nil {
					w.favorites.refresh()
				}
			}
			recentChanged := false
			for i, item := range w.recent {
				if updated, changed := withCountryCode(item.Station, codes); changed {
					w.recent[i].Station = updated
					recentChanged = true
				}
			}
			if recentChanged {
				if err := w.store.SaveRecent(w.recent); err != nil {
					slog.Warn("could not save recent station country codes", "error", err)
				}
				if w.recentSection != nil {
					w.recentSection.refresh()
				}
			}
		})
	}()
}
