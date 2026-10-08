package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"go.lucor.dev/fynetune/internal/radio"
)

// systemTray owns the desktop tray icon and its playback menu.
type systemTray struct {
	desktopApp desktop.App
	window     fyne.Window
	onToggle   func()
	onQuit     func()
	station    *fyne.MenuItem
	track      *fyne.MenuItem
	play       *fyne.MenuItem
	menu       *fyne.Menu
}

// newSystemTray creates the desktop tray menu and associates it with the window.
func newSystemTray(desktopApp desktop.App, window fyne.Window, icon fyne.Resource, onToggle, onQuit func()) *systemTray {
	t := &systemTray{
		desktopApp: desktopApp,
		window:     window,
		onToggle:   onToggle,
		onQuit:     onQuit,
	}
	t.update(radio.Station{}, "Not playing", radio.StateStopped)
	desktopApp.SetSystemTrayIcon(theme.NewThemedResource(icon))
	return t
}

// update refreshes the station, track, and play or stop menu items.
func (t *systemTray) update(station radio.Station, track string, state radio.PlayerState) {
	stationName := "No station selected"
	if station.ID != "" {
		stationName = station.Name
	}
	if track == "" {
		track = "Not playing"
	}
	playLabel := "Play"
	if state != radio.StateStopped && state != radio.StateError {
		playLabel = "Stop"
	}
	if t.menu == nil {
		t.station = fyne.NewMenuItem(stationName, nil)
		t.station.Disabled = true
		t.track = fyne.NewMenuItem(track, nil)
		t.track.Disabled = true
		t.play = fyne.NewMenuItem(playLabel, t.onToggle)
		t.menu = fyne.NewMenu("FyneTune",
			t.station,
			t.track,
			fyne.NewMenuItemSeparator(),
			t.play,
			fyne.NewMenuItem("Show Window", t.window.Show),
			fyne.NewMenuItem("Quit", t.onQuit),
		)
	} else if t.station.Label == stationName && t.track.Label == track && t.play.Label == playLabel {
		return
	}
	t.station.Label = stationName
	t.track.Label = track
	t.play.Label = playLabel
	t.desktopApp.SetSystemTrayMenu(t.menu)
}
