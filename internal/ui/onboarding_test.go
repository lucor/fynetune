package ui

import (
	"errors"
	"image/color"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/ui/navigation"
)

func TestOnboardingCanvasColorsFollowThemeChanges(t *testing.T) {
	app := test.NewTempApp(t)
	appTheme := &brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceDark}
	app.Settings().SetTheme(appTheme)
	o := newOnboarding(&Window{app: app})
	o.welcomeScreen()

	if got, want := color.NRGBAModel.Convert(o.choiceLabel.Color).(color.NRGBA), color.NRGBAModel.Convert(theme.Color(theme.ColorNameForeground)).(color.NRGBA); got != want {
		t.Fatalf("dark welcome text color = %#v, want %#v", got, want)
	}
	if got, want := color.NRGBAModel.Convert(o.themeRects[0].rect.FillColor).(color.NRGBA), color.NRGBAModel.Convert(theme.Color(theme.ColorNameInputBackground)).(color.NRGBA); got != want {
		t.Fatalf("dark country card color = %#v, want %#v", got, want)
	}

	appTheme.appearance = appearanceLight
	app.Settings().SetTheme(appTheme)
	o.refreshTheme()
	if got, want := color.NRGBAModel.Convert(o.choiceLabel.Color).(color.NRGBA), color.NRGBAModel.Convert(theme.Color(theme.ColorNameForeground)).(color.NRGBA); got != want {
		t.Fatalf("light welcome text color = %#v, want %#v", got, want)
	}
	if got, want := color.NRGBAModel.Convert(o.themeRects[0].rect.FillColor).(color.NRGBA), color.NRGBAModel.Convert(theme.Color(theme.ColorNameInputBackground)).(color.NRGBA); got != want {
		t.Fatalf("light country card color = %#v, want %#v", got, want)
	}
}

func TestCountryCodeFromLocale(t *testing.T) {
	tests := []struct {
		locale string
		want   string
	}{
		{locale: "it_IT.UTF-8", want: "IT"},
		{locale: "en-US", want: "US"},
		{locale: "en", want: ""},
		{locale: "C", want: ""},
	}
	for _, test := range tests {
		t.Run(test.locale, func(t *testing.T) {
			if got := countryCodeFromLocale(test.locale); got != test.want {
				t.Fatalf("countryCodeFromLocale(%q) = %q, want %q", test.locale, got, test.want)
			}
		})
	}
}

// TestSettingsCountryChange keeps the current view and data until loading succeeds.
func TestSettingsCountryChange(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "network error"
		}
		t.Run(name, func(t *testing.T) {
			base := test.NewTempApp(t)
			base.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
			driver := &startupTestDriver{Driver: base.Driver(), completed: make(chan func(), 1)}
			app := &startupTestApp{App: base, driver: driver}
			fyne.SetCurrentApp(app)
			t.Cleanup(func() { fyne.SetCurrentApp(base) })
			gate := make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-gate:
				default:
					close(gate)
				}
			})
			directory := &startupTestDirectory{gate: gate, stations: []radio.Station{{ID: "new"}}}
			if fail {
				directory.err = errors.New("network unavailable")
			}
			w := &Window{app: app, window: test.NewTempWindow(t, nil), directory: directory, store: storage.New(app)}
			w.settings.DiscoveryCountry = "@US"
			if err := w.store.SaveSettings(w.settings); err != nil {
				t.Fatal(err)
			}
			w.home = newHomeSection(w)
			w.home.setStations([]radio.Station{{ID: "previous"}})
			items := make([]navigation.Item, 5)
			for i := range items {
				items[i] = navigation.Item{Icon: homeBrandIcon}
			}
			w.bottomNavigation = navigation.New(items...)
			w.bottomNavigation.Select(4)
			w.appContent = widget.NewLabel("App")
			picker := widget.NewLabel("Country picker")
			w.setContent(picker)
			w.window.Resize(fyne.NewSize(480, 800))
			w.window.Show()
			o := &onboarding{window: w, fromSettings: true, selected: Country{Code: "IT", Name: "Italy"}}
			w.onboarding = o
			o.startCountrySetup("IT")
			if w.window.Content() != picker || o.loadingDialog == nil {
				t.Fatal("country loading must use a modal over the current view")
			}
			if w.settings.DiscoveryCountry != "@US" || w.home.rows.entries[0].station.ID != "previous" {
				t.Fatal("loading must preserve the current country and stations")
			}
			o.startCountrySetup("IT") // The modal also prevents a second loading action.
			close(gate)
			waitStartup(t, driver.completed)
			if directory.calls.Load() != 1 {
				t.Fatal("country change must fetch once")
			}
			saved, err := w.store.Load()
			if err != nil {
				t.Fatal(err)
			}
			if fail {
				if saved.DiscoveryCountry != "@US" || w.settings.DiscoveryCountry != "@US" || w.home.rows.entries[0].station.ID != "previous" || w.window.Content() != picker {
					t.Fatal("failed country change must preserve the saved country, list, and picker")
				}
			} else if saved.DiscoveryCountry != "@IT" || w.home.rows.entries[0].station.ID != "new" || w.window.Content() != w.appContent || w.bottomNavigation.Selected != 4 {
				t.Fatal("successful country change must save the new country, update Radio, and stay in Settings")
			}
		})
	}
}

func TestCountryPickerRequiresExplicitSelection(t *testing.T) {
	_, onboarding := newOnboardingTestWindow(t)
	onboarding.setCountries([]radiobrowser.Country{
		{Code: "US", Name: "United States"},
		{Code: "CA", Name: "Canada"},
	}, nil)

	if onboarding.selected.Code != "" {
		t.Fatalf("selected country = %q before user selection, want empty", onboarding.selected.Code)
	}
	test.Tap(onboarding.chooseCountry)
	if !onboarding.continueCountry.Disabled() {
		t.Fatal("Continue should be disabled before selecting a country")
	}
	if onboarding.selected.Code != "" {
		t.Fatalf("selected country = %q when opening picker, want empty", onboarding.selected.Code)
	}

	onboarding.list.Select(0)
	if got, want := onboarding.selected.Code, "CA"; got != want {
		t.Fatalf("selected country = %q, want %q", got, want)
	}
	if onboarding.continueCountry.Disabled() {
		t.Fatal("Continue should be enabled after selecting a country")
	}

	onboarding.search.SetText("United")
	if onboarding.selected.Code != "" {
		t.Fatalf("selected country = %q after changing search, want empty", onboarding.selected.Code)
	}
	if !onboarding.continueCountry.Disabled() {
		t.Fatal("Continue should be disabled after changing search")
	}
}

func TestCountryPickerCanBeOpenedAgainAfterBack(t *testing.T) {
	window, onboarding := newOnboardingTestWindow(t)
	onboarding.setCountries([]radiobrowser.Country{
		{Code: "US", Name: "United States"},
		{Code: "CA", Name: "Canada"},
	}, nil)
	test.Tap(onboarding.chooseCountry)

	back := findOnboardingButton(window.Content(), "Back")
	if back == nil {
		t.Fatal("Back button not found")
	}
	test.Tap(back)
	if onboarding.chooseCountry.Disabled() {
		t.Fatal("Choose another country should be enabled after returning to welcome")
	}
	test.Tap(onboarding.chooseCountry)
	if !onboarding.continueCountry.Disabled() {
		t.Fatal("Continue should be disabled when reopening the country picker")
	}
	if onboarding.selected.Code != "" {
		t.Fatalf("selected country = %q when reopening picker, want empty", onboarding.selected.Code)
	}
}

func TestOnboardingLoadingScreenUpdatesWhenStationsFinishLoading(t *testing.T) {
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, widget.NewLabel(""))
	window.Resize(fyne.NewSize(480, 800))
	w := &Window{app: app, window: window, store: storage.New(app)}
	o := &onboarding{window: w, pendingCode: "IT", suggested: Country{Code: "IT", Name: "Italy"}, selected: Country{Code: "IT", Name: "Italy"}}
	w.onboarding = o
	o.showCountryLoading()
	window.Show()
	loadingContent := window.Content()

	if !o.loadActivityBox.Visible() {
		t.Fatal("activity indicator should be visible while stations are loading")
	}
	if o.loadContinueBox.Visible() {
		t.Fatal("Continue should stay hidden while stations are loading")
	}
	countrySegment, ok := o.loadDescription.Segments[1].(*widget.TextSegment)
	if !ok || !countrySegment.Style.TextStyle.Bold || countrySegment.Text != "Italy" {
		t.Fatal("loading message should highlight the country name in bold")
	}

	o.finishCountrySetup(nil, nil)
	if w.initialStations == nil {
		t.Fatal("an empty successful response must be ready without another station request")
	}
	settings, err := w.store.Load()
	if err != nil || settings.DiscoveryCountry != "@IT" {
		t.Fatalf("saved country = %q, error = %v", settings.DiscoveryCountry, err)
	}
	query, popular := radioStationQuery("@IT")
	var cached []radio.Station
	result, err := cache.New(app.Cache()).Get(radioStationCacheKey(query, popular), radioCacheTTL, &cached)
	if err != nil || !result.Fresh || len(cached) != 0 {
		t.Fatal("successful onboarding must cache even an empty station list for the next startup")
	}

	if window.Content() != loadingContent {
		t.Fatal("finishing station loading should keep the same onboarding screen")
	}
	if o.loadActivityBox.Visible() {
		t.Fatal("activity indicator should be hidden after stations finish loading")
	}
	if !o.loadContinueBox.Visible() {
		t.Fatal("Continue should appear after stations finish loading")
	}
	if got, want := o.loadDescription.String(), "Stations are ready for Italy."; got != want {
		t.Fatalf("completion message = %q, want %q", got, want)
	}
	if got, want := o.loadSteps[1].label.Text, "Loaded stations for Italy"; got != want {
		t.Fatalf("station status = %q, want %q", got, want)
	}
	if got, want := o.loadSteps[2].label.Text, "Radio experience ready"; got != want {
		t.Fatalf("final status = %q, want %q", got, want)
	}
}

// TestOnboardingFailedSetup preserves existing data and offers retry instead of Continue.
func TestOnboardingFailedSetup(t *testing.T) {
	app := test.NewTempApp(t)
	w := &Window{window: test.NewTempWindow(t, nil), store: storage.New(app)}
	w.settings.DiscoveryCountry = "@US"
	w.initialStations = []radio.Station{{ID: "previous"}}
	if err := w.store.SaveSettings(w.settings); err != nil {
		t.Fatal(err)
	}
	o := &onboarding{window: w, pendingCode: "IT", selected: Country{Code: "IT", Name: "Italy"}}
	w.onboarding = o
	o.showCountryLoading()
	o.finishCountrySetup(nil, errors.New("network unavailable"))
	if o.loadContinue.Text != "Retry" || !o.loadContinueBox.Visible() {
		t.Fatal("failed setup must offer Retry instead of Continue")
	}
	if !o.loadError.Visible() || o.loadActivityBox.Visible() {
		t.Fatal("failed setup must show its error and stop the loading indicator")
	}
	settings, err := w.store.Load()
	if err != nil || settings.DiscoveryCountry != "@US" || w.settings.DiscoveryCountry != "@US" {
		t.Fatal("failed setup must preserve the previous country")
	}
	if len(w.initialStations) != 1 || w.initialStations[0].ID != "previous" {
		t.Fatal("failed setup must preserve the previous stations")
	}
	// With no directory, retry must remain in onboarding and fail again safely.
	test.Tap(o.loadContinue)
	if w.onboarding != o || o.loadContinue.Text != "Retry" {
		t.Fatal("retry must keep onboarding active until setup succeeds")
	}
}

func newOnboardingTestWindow(t *testing.T) (fyne.Window, *onboarding) {
	t.Helper()
	app := test.NewTempApp(t)
	app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
	window := test.NewTempWindow(t, widget.NewLabel(""))
	window.Resize(fyne.NewSize(480, 800))
	w := &Window{window: window}
	o := &onboarding{window: w, suggestedCode: "US"}
	w.onboarding = o
	window.SetContent(o.welcomeScreen())
	window.Show()
	return window, o
}

func findOnboardingButton(object fyne.CanvasObject, text string) *widget.Button {
	if button, ok := object.(*widget.Button); ok && button.Text == text {
		return button
	}
	parent, ok := object.(*fyne.Container)
	if !ok {
		return nil
	}
	for _, child := range parent.Objects {
		if button := findOnboardingButton(child, text); button != nil {
			return button
		}
	}
	return nil
}
