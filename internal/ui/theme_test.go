package ui

import (
	"image/color"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
)

func TestAppControlsFollowAppearanceChanges(t *testing.T) {
	app := test.NewTempApp(t)
	appTheme := &brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceDark}
	app.Settings().SetTheme(appTheme)
	window := test.NewTempWindow(t, nil)
	w := &Window{
		app: app, window: window, theme: appTheme, store: storage.New(app),
		radioPlayer: &trayTestPlayer{}, settings: storage.Settings{Appearance: appearanceDark},
	}
	w.appContent = newApp(w)
	stations := []radio.Station{{ID: "station", Name: "Station", Tags: []string{"Jazz"}, Codec: "MP3", Bitrate: 128}}
	w.home.rows.setStations(stations)
	w.discover.rows.setStations(stations)
	w.discover.results.Add(w.discover.rows.list)
	window.SetContent(w.appContent)
	window.Resize(fyne.NewSize(320, 520))
	window.Show()
	w.bottomNavigation.Select(1)

	for _, appearance := range []string{appearanceDark, appearanceLight, appearanceDark} {
		t.Run(appearance, func(t *testing.T) {
			w.setAppearance(appearance)
			for name, resource := range map[string]fyne.Resource{
				"add station": w.favorites.add.Icon,
				"volume":      w.playerBar.mute.Icon,
				"favorites":   w.bottomNavigation.Items[1].Icon,
				"recent":      w.bottomNavigation.Items[2].Icon,
				"discover":    w.bottomNavigation.Items[3].Icon,
				"settings":    w.bottomNavigation.Items[4].Icon,
			} {
				prefix := "lucide-" + appearance + "-"
				if got := resource.Name(); !strings.HasPrefix(got, prefix) {
					t.Errorf("%s icon = %q, want the %s variant", name, got, appearance)
				}
			}
			for _, icon := range w.themeIcons {
				if got, want := icon.widget.Resource.Name(), icon.resource().Name(); got != want {
					t.Errorf("registered icon = %q, want %q", got, want)
				}
			}
			assertRenderedTextColor(t, w.sectionHeader, theme.Color(theme.ColorNameForeground))
			assertRenderedTextColor(t, w.favorites.subtitle, theme.Color(theme.ColorNameForeground))
			assertRenderedTextColor(t, w.home.subtitle, theme.Color(stationMetadataColorName))
			assertRenderedTextColor(t, w.playerBar.title, theme.Color(theme.ColorNameForeground))
			assertRenderedTextColor(t, w.playerBar.track, theme.Color(stationMetadataColorName))
			walkSettingsObjects(w.favorites.empty, func(object fyne.CanvasObject) {
				if button, ok := object.(*widget.Button); ok && button.Text == "Add station" {
					assertRenderedTextColor(t, button, theme.Color(theme.ColorNameForegroundOnPrimary))
				}
			})
			for index, rows := range map[int]*stationList{0: w.home.rows, 3: w.discover.rows} {
				w.bottomNavigation.Select(index)
				found := false
				for _, row := range rows.rows {
					if row.station.ID != "station" {
						continue
					}
					found = true
					for _, button := range []*widget.Button{row.favorite, row.manage} {
						if got := button.Icon.Name(); !strings.HasPrefix(got, "lucide-"+appearance+"-") {
							t.Errorf("station action icon = %q, want the %s variant", got, appearance)
						}
					}
					assertRenderedTextColor(t, row.title, theme.Color(theme.ColorNameForeground))
					assertRenderedTextColor(t, row.description, theme.Color(stationMetadataColorName))
				}
				if !found {
					t.Fatal("station rows were not rendered")
				}
			}
			w.bottomNavigation.Select(1)
		})
	}
}

func TestOnboardingSupportingTextFollowsAppearanceChanges(t *testing.T) {
	app := test.NewTempApp(t)
	appTheme := &brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceDark}
	app.Settings().SetTheme(appTheme)
	window := test.NewTempWindow(t, nil)
	w := &Window{app: app, window: window, theme: appTheme, store: storage.New(app)}
	w.onboarding = newOnboarding(w)
	window.SetContent(w.onboarding.welcomeScreen())
	window.Resize(fyne.NewSize(320, 520))
	window.Show()

	for _, appearance := range []string{appearanceDark, appearanceLight} {
		t.Run(appearance, func(t *testing.T) {
			w.setAppearance(appearance)
			found := false
			walkSettingsObjects(window.Content(), func(object fyne.CanvasObject) {
				if label, ok := object.(*widget.Label); ok && label.Text == "Most popular stations in your area" {
					found = true
					assertRenderedTextColor(t, label, theme.Color(stationMetadataColorName))
				}
			})
			if !found {
				t.Fatal("country suggestion description is missing")
			}
		})
	}
}

func assertRenderedTextColor(t *testing.T, object fyne.CanvasObject, want color.Color) {
	t.Helper()
	found := false
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		switch object := object.(type) {
		case *canvas.Text:
			if object.Text == "" {
				return
			}
			found = true
			if got := color.NRGBAModel.Convert(object.Color); got != color.NRGBAModel.Convert(want) {
				t.Errorf("%q color = %v, want %v", object.Text, got, want)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(object).Objects() {
				visit(child)
			}
		case *fyne.Container:
			for _, child := range object.Objects {
				visit(child)
			}
		}
	}
	visit(object)
	if !found {
		t.Error("no rendered text found")
	}
}
