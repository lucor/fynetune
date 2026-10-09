package ui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/ui/widgets"
	"go.lucor.dev/fynetune/internal/version"
)

// settingsPage groups radio, appearance, playback, and startup preferences.
func (w *Window) settingsPage() fyne.CanvasObject {
	appearance := widget.NewSelect([]string{"System", "Light", "Dark"}, func(value string) {
		w.setAppearance(appearanceValue(value))
	})
	appearance.SetSelected(appearanceLabel(w.settings.Appearance))
	appearanceRow := w.settingsRow(widget.NewIcon(theme.ColorPaletteIcon()),
		settingsText("Theme", "Choose the application theme"), appearance)

	auto := w.settingsCheckRow(theme.MediaPlayIcon(), "Play last station on startup",
		"Automatically resume the last played station", w.settings.AutoPlay, func(value bool) {
			w.settings.AutoPlay = value
			w.saveSettings()
		})

	reconnect := w.settingsCheckRow(theme.MediaReplayIcon(), "Reconnect automatically",
		"Try to reconnect when the stream is interrupted", w.settings.Reconnect, func(value bool) {
			w.settings.Reconnect = value
			w.radioPlayer.SetReconnect(value)
			w.saveSettings()
		})

	startup := []fyne.CanvasObject{auto}
	if !w.app.Driver().Device().IsMobile() {
		minimized := w.settingsCheckRow(theme.WindowMinimizeIcon(), "Start minimized",
			"Launch FyneTune in the background", w.settings.StartMinimized, func(value bool) {
				w.settings.StartMinimized = value
				w.saveSettings()
			})
		startup = append(startup, minimized)
	}

	info := version.Current()
	websiteURL := &url.URL{Scheme: "https", Host: "fynetune.lucor.dev"}
	w.settingsVersionBadge = widgets.NewBadge(info.Version,
		theme.Color(theme.ColorNameSelection), theme.Color(stationMetadataColorName))
	appName := widget.NewLabelWithStyle("FyneTune", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	aboutDescription := widget.NewLabel("A simple and modern internet radio player")
	aboutDescription.Wrapping = fyne.TextWrapWord
	aboutDescription.Importance = widget.LowImportance
	aboutDescription.SizeName = stationMetadataSizeName
	about := w.settingsCard(container.NewVBox(
		container.NewBorder(nil, nil,
			container.NewGridWrap(fyne.NewSize(48, 48), widget.NewIcon(homeBrandIcon)), nil,
			container.NewThemeOverride(
				container.NewVBox(container.NewHBox(appName, container.NewCenter(w.settingsVersionBadge)), aboutDescription),
				compactTextTheme{Theme: w.app.Settings().Theme()})),
		widget.NewSeparator(),
		widget.NewHyperlink("fynetune.lucor.dev", websiteURL),
	))
	country := discoveryCountry(w.settings.DiscoveryCountry)
	countryFlag := canvas.NewText(countryOptionFlag(country.Code), theme.Color(theme.ColorNameForeground))
	countryFlag.TextSize = theme.Size(theme.SizeNameHeadingText)
	countryText := settingsText(country.Name, "Current country for Radio tab")
	countryName := countryText.Objects[0].(*widget.Label)
	changeCountry := widget.NewButton("Change", w.changeCountry)
	if w.directory == nil {
		changeCountry.Disable()
	}
	w.settingsCountryFlag = countryFlag
	w.settingsCountryName = countryName
	countryRow := w.settingsRow(countryFlag, countryText, changeCountry)

	settingsContent := []fyne.CanvasObject{
		w.settingsHeading("Radio", homeBrandIcon),
		countryRow,
		widget.NewSeparator(),
		w.settingsHeading("Appearance", theme.ColorPaletteIcon()),
		appearanceRow,
		widget.NewSeparator(),
		w.settingsHeading("Playback", theme.MediaPlayIcon()),
		reconnect,
		widget.NewSeparator(),
		w.settingsHeading("Startup", theme.LogoutIcon()),
	}
	settingsContent = append(settingsContent, startup...)
	settingsContent = append(settingsContent,
		widget.NewSeparator(),
		w.settingsHeading("About", theme.InfoIcon()),
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

// settingsCheckRow places a standard check beside an icon and explanatory text.
func (w *Window) settingsCheckRow(icon fyne.Resource, title, description string, checked bool, onChanged func(bool)) fyne.CanvasObject {
	check := widget.NewCheck("", onChanged)
	check.SetChecked(checked)
	return w.settingsRow(widget.NewIcon(icon), settingsText(title, description),
		container.NewGridWrap(fyne.NewSize(48, 48), container.NewCenter(check)))
}

// settingsHeading identifies a preference group with a standard themed icon.
func (w *Window) settingsHeading(title string, icon fyne.Resource) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	label.Importance = widget.LowImportance
	return container.NewThemeOverride(
		container.NewHBox(widget.NewIcon(theme.NewThemedResource(icon)), label),
		compactTextTheme{Theme: w.app.Settings().Theme()})
}

// settingsText stacks a setting's title and smaller supporting text.
func settingsText(title, description string) *fyne.Container {
	label := widget.NewLabel(title)
	label.Wrapping = fyne.TextWrapWord
	detail := widget.NewLabel(description)
	detail.Wrapping = fyne.TextWrapWord
	detail.Importance = widget.LowImportance
	detail.SizeName = stationMetadataSizeName
	return container.NewVBox(label, detail)
}

// settingsRow keeps icons and controls vertically centered beside the text.
func (w *Window) settingsRow(icon, text, control fyne.CanvasObject) fyne.CanvasObject {
	return w.settingsCard(container.NewBorder(nil, nil,
		container.NewCenter(container.NewGridWrap(fyne.NewSize(28, 28), icon)),
		container.NewCenter(control),
		container.NewThemeOverride(text, compactTextTheme{Theme: w.app.Settings().Theme()})))
}

// settingsCard uses Fyne's card renderer for rounded, theme-aware settings panels.
func (w *Window) settingsCard(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewThemeOverride(widget.NewCard("", "", content),
		settingsCardTheme{Theme: w.app.Settings().Theme()})
}

// refreshSettingsTheme updates the version badge when the appearance changes.
func (w *Window) refreshSettingsTheme() {
	if w.settingsVersionBadge != nil {
		w.settingsVersionBadge.SetColors(theme.Color(theme.ColorNameSelection), theme.Color(stationMetadataColorName))
	}
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
