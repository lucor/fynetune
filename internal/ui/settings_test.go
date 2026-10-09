package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/storage"
)

// TestSettingsControlsPersistPreferences checks that the redesigned controls
// still update playback preferences and the stored application appearance.
func TestSettingsControlsPersistPreferences(t *testing.T) {
	app := test.NewTempApp(t)
	th := &brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight}
	app.Settings().SetTheme(th)
	window := test.NewTempWindow(t, nil)
	w := &Window{app: app, window: window, theme: th, store: storage.New(app), radioPlayer: &trayTestPlayer{},
		settings: storage.Settings{DiscoveryCountry: "@IT", Reconnect: true, Volume: .75, Appearance: appearanceLight}}
	page := w.settingsPage()
	window.SetContent(page)
	window.Resize(fyne.NewSize(480, 800))
	window.Show()
	checks := make(map[string]*widget.Check)
	var appearance *widget.Select
	walkSettingsObjects(page, func(obj fyne.CanvasObject) {
		if selectWidget, ok := obj.(*widget.Select); ok {
			appearance = selectWidget
		}
		if card, ok := obj.(*widget.Card); ok {
			var title string
			var check *widget.Check
			walkSettingsObjects(card.Content, func(child fyne.CanvasObject) {
				if label, ok := child.(*widget.Label); ok && title == "" {
					title = label.Text
				}
				if value, ok := child.(*widget.Check); ok {
					check = value
				}
			})
			if check != nil {
				checks[title] = check
			}
		}
	})
	for _, title := range []string{"Reconnect automatically", "Play last station on startup", "Start minimized"} {
		check := checks[title]
		if check == nil {
			t.Fatalf("missing control for %q", title)
		}
		test.Tap(check)
	}
	if appearance == nil || appearance.Selected != "Light" {
		t.Fatal("appearance must reflect the saved setting")
	}
	appearance.SetSelected("Dark")
	saved, err := w.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Reconnect || !saved.AutoPlay || !saved.StartMinimized || saved.Appearance != appearanceDark {
		t.Fatalf("settings controls did not persist their changes: %+v", saved)
	}
	if saved.DiscoveryCountry != "@IT" || saved.Volume != .75 {
		t.Fatal("editing preferences must preserve the country and volume")
	}
	w.settings.DiscoveryCountry = "@US"
	w.refreshSettingsCountry()
	if w.settingsCountryName.Text != discoveryCountry("@US").Name || w.settingsCountryFlag.Text != countryOptionFlag("US") {
		t.Fatal("Settings must show the updated display name and flag after changing country")
	}
}

// walkSettingsObjects visits the native containers and cards holding preferences.
func walkSettingsObjects(obj fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	visit(obj)
	switch obj := obj.(type) {
	case *fyne.Container:
		for _, child := range obj.Objects {
			walkSettingsObjects(child, visit)
		}
	case *container.ThemeOverride:
		walkSettingsObjects(obj.Content, visit)
	case *container.Scroll:
		walkSettingsObjects(obj.Content, visit)
	case *widget.Card:
		walkSettingsObjects(obj.Content, visit)
	}
}
