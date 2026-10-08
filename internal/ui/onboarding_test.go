package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

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
	app.Settings().SetTheme(brandTheme{Theme: theme.LightTheme()})
	window := test.NewTempWindow(t, widget.NewLabel(""))
	window.Resize(fyne.NewSize(480, 800))
	w := &Window{window: window}
	o := &onboarding{window: w, suggested: Country{Code: "IT", Name: "Italy"}, selected: Country{Code: "IT", Name: "Italy"}}
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

func newOnboardingTestWindow(t *testing.T) (fyne.Window, *onboarding) {
	t.Helper()
	app := test.NewTempApp(t)
	app.Settings().SetTheme(brandTheme{Theme: theme.LightTheme()})
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
