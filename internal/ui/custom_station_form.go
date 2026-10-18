package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
)

// addCustomStation opens the form for a station entered by the user.
func (w *Window) addCustomStation() {
	w.showCustomStationForm(radio.Station{}, false)
}

// editCustomStation opens the form with the saved station details.
func (w *Window) editCustomStation(station radio.Station) {
	w.showCustomStationForm(station, true)
}

// showCustomStationForm builds the shared add and edit station form.
func (w *Window) showCustomStationForm(existing radio.Station, editing bool) {
	name := widget.NewEntry()
	name.SetPlaceHolder("My favorite radio")
	name.SetText(existing.Name)
	name.Validator = func(value string) error {
		if strings.TrimSpace(value) == "" {
			return radio.ErrStationNameRequired
		}
		return nil
	}

	streamURL := widget.NewEntry()
	streamURL.SetPlaceHolder("https://example.com/stream")
	streamURL.SetText(existing.URL)
	streamURL.Validator = func(value string) error {
		return radio.ValidateStation(radio.Station{Name: "Station", URL: value})
	}

	website := widget.NewEntry()
	website.SetPlaceHolder("https://example.com")
	website.SetText(existing.Homepage)

	title, confirm := "Add station", "Add station"
	if editing {
		title, confirm = "Edit station", "Save"
	}
	form := widget.NewForm(
		widget.NewFormItem("Station name", name),
		widget.NewFormItem("Stream URL", streamURL),
		widget.NewFormItem("Website (optional)", website),
	)
	form.Orientation = widget.Vertical
	status := widget.NewLabel("")
	status.Wrapping = fyne.TextWrapWord
	save := widget.NewButton(confirm, func() {
		if err := form.Validate(); err != nil {
			status.SetText(err.Error())
			return
		}
		var station radio.Station
		var err error
		if editing {
			station, err = radio.UpdateCustomStation(existing, name.Text, streamURL.Text, website.Text, w.settings.Stations)
		} else {
			station, err = radio.NewCustomStation(name.Text, streamURL.Text, website.Text, w.settings.Stations)
		}
		if err != nil {
			status.SetText(err.Error())
			return
		}
		if err := w.saveCustomStation(station, editing); err != nil {
			status.SetText(err.Error())
			return
		}
		w.setContent(w.appContent)
	})
	save.Importance = widget.HighImportance
	back := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		w.setContent(w.appContent)
	})
	back.Importance = widget.LowImportance
	back.Resize(fyne.NewSize(40, 40))
	description := widget.NewLabel("Add a radio station using its stream URL.")
	description.Wrapping = fyne.TextWrapWord
	if editing {
		description.SetText("Update the station name, stream URL, or website.")
	}
	heading := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	heading.SizeName = theme.SizeNameHeadingText
	top := container.NewVBox(
		container.NewHBox(back, heading),
		description,
	)
	bottom := container.NewHBox(
		layout.NewSpacer(),
		widget.NewButton("Cancel", func() { w.setContent(w.appContent) }),
		save,
	)
	fields := container.NewVBox(form, status)
	w.setContent(container.NewPadded(container.NewBorder(top, bottom, nil, nil, container.NewVScroll(fields))))
}

// saveCustomStation persists the station before exposing it in Favorites.
func (w *Window) saveCustomStation(station radio.Station, editing bool) error {
	stations := append([]radio.Station(nil), w.settings.Stations...)
	recent := append([]storage.RecentStation(nil), w.recent...)
	if editing {
		found := false
		for i := range stations {
			if stations[i].ID == station.ID {
				stations[i] = station
				found = true
				break
			}
		}
		if !found {
			return radio.ErrNotCustomStation
		}
		for i := range recent {
			if recent[i].Station.ID == station.ID {
				recent[i].Station = station
			}
		}
	} else {
		stations = append(stations, station)
	}
	if err := w.store.SaveStations(stations); err != nil {
		return err
	}
	if editing {
		if err := w.store.SaveRecent(recent); err != nil {
			if rollbackErr := w.store.SaveStations(w.settings.Stations); rollbackErr != nil {
				return fmt.Errorf("%w (also failed to restore favorites: %v)", err, rollbackErr)
			}
			return err
		}
	}
	w.settings.Stations = stations
	if editing {
		w.recent = recent
		if w.selectedStation.ID == station.ID {
			streamChanged := w.selectedStation.URL != station.URL
			w.selectedStation = station
			playing := w.radioPlayer != nil && w.radioPlayer.State() != radio.StateStopped
			if w.playerBar != nil {
				status := "Not playing"
				if playing {
					status = "Connecting…"
				}
				w.playerBar.setPlayback(station, status, playing)
			}
			if playing && streamChanged {
				w.radioPlayer.Play(station)
				w.updateTray()
			}
		}
	}
	if w.favorites != nil {
		w.favorites.refresh()
	}
	if w.home != nil {
		w.home.rows.list.Refresh()
	}
	if w.discover != nil {
		w.discover.rows.refreshFavorites()
	}
	if w.recentSection != nil {
		w.recentSection.refresh()
	}
	return nil
}
