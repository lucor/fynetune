package ui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/version"
)

// settingsPage shows the persisted startup and playback preferences.
func (w *Window) settingsPage() fyne.CanvasObject {
	appearance := widget.NewSelect([]string{"System", "Light", "Dark"}, func(value string) {
		w.setAppearance(appearanceValue(value))
	})
	appearance.SetSelected(appearanceLabel(w.settings.Appearance))
	appearanceRow := container.NewBorder(nil, nil, widget.NewLabel("Theme"), appearance, nil)

	auto := settingsCheckRow("Play last station on startup", w.settings.AutoPlay, func(value bool) {
		w.settings.AutoPlay = value
		w.saveSettings()
	})

	reconnect := settingsCheckRow("Reconnect automatically", w.settings.Reconnect, func(value bool) {
		w.settings.Reconnect = value
		w.radioPlayer.SetReconnect(value)
		w.saveSettings()
	})

	startup := []fyne.CanvasObject{auto}
	if !w.app.Driver().Device().IsMobile() {
		minimized := settingsCheckRow("Start minimized", w.settings.StartMinimized, func(value bool) {
			w.settings.StartMinimized = value
			w.saveSettings()
		})
		startup = append(startup, minimized)
	}

	info := version.Current()
	websiteURL := &url.URL{Scheme: "https", Host: "fynetune.lucor.dev"}
	versionRow := container.NewHBox(
		widget.NewLabel("Version"),
		widget.NewLabelWithStyle(info.Version, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	)
	aboutDetails := container.NewVBox(
		versionRow,
		widget.NewHyperlink("fynetune.lucor.dev", websiteURL),
	)
	about := container.NewVBox(
		widget.NewLabelWithStyle("About", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		aboutDetails,
	)
	country := discoveryCountry(w.settings.DiscoveryCountry)
	countryFlag := canvas.NewText(countryOptionFlag(country.Code), theme.Color(theme.ColorNameForeground))
	countryFlag.TextSize = theme.Size(theme.SizeNameHeadingText)
	countryName := widget.NewLabel(country.Name)
	changeCountry := widget.NewButton("Change", w.changeCountry)
	if w.directory == nil {
		changeCountry.Disable()
	}
	w.settingsCountryFlag = countryFlag
	w.settingsCountryName = countryName
	countryRow := container.NewBorder(nil, nil,
		container.NewGridWrap(fyne.NewSize(48, 48), container.NewCenter(countryFlag)),
		changeCountry,
		countryName,
	)

	settingsContent := []fyne.CanvasObject{
		widget.NewLabelWithStyle("Radio", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		countryRow,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Appearance", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		appearanceRow,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Playback", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		reconnect,
		widget.NewSeparator(),
		widget.NewLabelWithStyle("Startup", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	}
	settingsContent = append(settingsContent, startup...)
	settingsContent = append(settingsContent,
		widget.NewSeparator(),
		about,
	)
	return container.NewVScroll(container.NewPadded(container.NewVBox(settingsContent...)))
}

// appearanceLabel returns the setting label for a stored appearance value.
func appearanceLabel(value string) string {
	switch value {
	case appearanceLight:
		return "Light"
	case appearanceDark:
		return "Dark"
	default:
		return "System"
	}
}

// appearanceValue converts the visible setting label to its persisted value.
func appearanceValue(label string) string {
	switch label {
	case "Light":
		return appearanceLight
	case "Dark":
		return appearanceDark
	default:
		return appearanceSystem
	}
}

// settingsCheckRow places a standard check widget at the right side of its label.
func settingsCheckRow(label string, checked bool, onChanged func(bool)) fyne.CanvasObject {
	check := widget.NewCheck("", onChanged)
	check.SetChecked(checked)
	touchArea := container.NewGridWrap(fyne.NewSize(48, 48), container.NewCenter(check))
	text := widget.NewLabel(label)
	text.Wrapping = fyne.TextWrapWord
	return container.NewBorder(nil, nil, nil, touchArea, text)
}

// saveSettings persists the current settings and reports storage errors.
func (w *Window) saveSettings() error {
	if err := w.store.SaveSettings(w.settings); err != nil {
		dialog.ShowError(err, w.window)
		return err
	}
	return nil
}

// refreshSettingsCountry updates the selected country shown in Settings.
func (w *Window) refreshSettingsCountry() {
	if w.settingsCountryFlag == nil || w.settingsCountryName == nil {
		return
	}
	country := discoveryCountry(w.settings.DiscoveryCountry)
	w.settingsCountryFlag.Text = countryOptionFlag(country.Code)
	w.settingsCountryFlag.Refresh()
	w.settingsCountryName.SetText(country.Name)
}
