package ui

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
)

// countrySetupTimeout bounds country and initial station requests during onboarding.
const countrySetupTimeout = 12 * time.Second

// onboarding holds the country setup view state and its owning window.
type onboarding struct {
	window          *Window
	fromSettings    bool
	pendingCode     string
	suggestedCode   string
	suggested       Country
	selected        Country
	countries       []Country
	filtered        []Country
	search          *widget.Entry
	list            *widget.List
	rows            map[fyne.CanvasObject]*countryRow
	choiceLabel     *canvas.Text
	chooseCountry   *widget.Button
	continueCountry *widget.Button
	loadActivity    *widget.Activity
	loadActivityBox *fyne.Container
	loadContent     *fyne.Container
	loadContinue    *widget.Button
	loadContinueBox *fyne.Container
	loadDescription *widget.RichText
	loadSteps       []*onboardingStep
	loadError       *widget.Label
	countryActivity *widget.Activity
}

// countryRow keeps the standard list row objects used to display a country.
type countryRow struct {
	container *fyne.Container
	flag      *canvas.Text
	name      *widget.Label
}

// onboardingStep holds the icon and label for one loading status.
type onboardingStep struct {
	icon  *widget.Icon
	label *widget.Label
}

// set updates the visible state of a loading step.
func (s *onboardingStep) set(icon fyne.Resource, text string, colorName fyne.ThemeColorName) {
	s.icon.SetResource(theme.NewColoredResource(icon, colorName))
	s.label.SetText(text)
}

// newOnboarding creates the country setup view using the system locale suggestion.
func newOnboarding(w *Window) *onboarding {
	return &onboarding{window: w, suggestedCode: systemLocaleCountryCode()}
}

// loadCountries fetches the country list while the welcome view is displayed.
func (o *onboarding) loadCountries() {
	if o.window.directory == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), countrySetupTimeout)
		defer cancel()
		countries, err := o.window.directory.Countries(ctx)
		fyne.Do(func() { o.setCountries(countries, err) })
	}()
}

// welcomeScreen builds the suggested-country page from the onboarding reference.
func (o *onboarding) welcomeScreen() fyne.CanvasObject {
	continueButton := widget.NewButton("Continue", func() {
		o.startCountrySetup(o.suggestedCode)
	})
	continueButton.Importance = widget.HighImportance
	o.chooseCountry = widget.NewButton("Choose another country", func() {
		o.showCountryPicker()
	})
	if o.countries == nil {
		o.chooseCountry.Disable()
	}
	countryName := o.suggestedCode
	if o.suggestedCode == "" {
		countryName = "Any country"
	} else if o.suggested.Name != "" {
		countryName = o.suggested.Name
	}
	o.choiceLabel = canvas.NewText(countryName, theme.Color(theme.ColorNameForeground))
	o.choiceLabel.TextSize = theme.Size(theme.SizeNameSubHeadingText)
	o.choiceLabel.TextStyle.Bold = true
	flagIcon := suggestedFlag(o.suggestedCode)
	selection := container.NewBorder(
		nil,
		nil,
		flagIcon,
		nil,
		container.NewVBox(
			o.choiceLabel,
			mutedLabel("Most popular stations in your area"),
		),
	)
	suggestedContent := container.NewPadded(container.NewVBox(
		widget.NewLabel("Suggested from your system"),
		selection,
	))
	suggestedBackground := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	suggestedBackground.CornerRadius = theme.Size(theme.SizeNameCardRadius)
	suggested := container.NewStack(suggestedBackground, suggestedContent)
	description := widget.NewLabel("Discover and listen to radio stations\nfrom around the world.")
	description.Alignment = fyne.TextAlignCenter
	description.Wrapping = fyne.TextWrapWord
	or := container.NewBorder(nil, nil, widget.NewSeparator(), widget.NewSeparator(),
		container.NewCenter(mutedLabel("or")))
	top := container.NewVBox(
		o.logo(),
		verticalSpace(24),
		o.welcomeTitle(),
		verticalSpace(4),
		description,
	)
	lower := container.NewVBox(
		container.NewCenter(container.NewGridWrap(fyne.NewSize(400, 132), suggested)),
		verticalSpace(24),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(400, 52), continueButton)),
		verticalSpace(16),
		or,
		verticalSpace(16),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(400, 50), o.chooseCountry)),
		verticalSpace(24),
		container.NewCenter(mutedLabel("You can change this later in Settings.")),
	)
	content := container.NewVBox(
		verticalSpace(68),
		top,
		verticalSpace(68),
		lower,
		layout.NewSpacer(),
	)
	return container.NewPadded(content)
}

// showCountryPicker displays the searchable list after country data is ready.
func (o *onboarding) showCountryPicker() {
	o.selected = Country{}
	o.search = widget.NewEntry()
	o.search.SetIcon(theme.SearchIcon())
	o.search.SetPlaceHolder("Search country…")
	o.filtered = append([]Country(nil), o.countries...)
	o.rows = make(map[fyne.CanvasObject]*countryRow)
	o.list = widget.NewList(
		func() int { return len(o.filtered) },
		func() fyne.CanvasObject {
			flag := canvas.NewText("", theme.Color(theme.ColorNameForeground))
			flag.TextSize = theme.Size(theme.SizeNameHeadingText)
			name := widget.NewLabel("")
			row := container.NewBorder(nil, nil,
				container.NewGridWrap(fyne.NewSize(48, 48), container.NewCenter(flag)),
				nil, name,
			)
			o.rows[row] = &countryRow{container: row, flag: flag, name: name}
			return row
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			country := o.filtered[id]
			row := o.rows[item]
			row.flag.Text = countryOptionFlag(country.Code)
			row.flag.Refresh()
			row.name.SetText(country.Name)
			row.container.Refresh()
		},
	)
	o.list.OnSelected = func(id widget.ListItemID) {
		if id >= len(o.filtered) {
			return
		}
		o.selected = o.filtered[id]
		o.continueCountry.Enable()
		o.list.Refresh()
	}
	o.search.OnChanged = func(query string) {
		query = strings.ToLower(strings.TrimSpace(query))
		o.selected = Country{}
		o.continueCountry.Disable()
		o.list.UnselectAll()
		o.filtered = o.filtered[:0]
		for _, country := range o.countries {
			if strings.Contains(strings.ToLower(country.Name), query) {
				o.filtered = append(o.filtered, country)
			}
		}
		o.list.Refresh()
	}
	backAction := func() { o.showWelcomeScreen() }
	if o.fromSettings {
		backAction = func() {
			o.window.onboarding = nil
			o.window.setContent(o.window.appContent)
		}
	}
	back := widget.NewButtonWithIcon("Back", theme.NavigateBackIcon(), backAction)
	back.Importance = widget.LowImportance
	back.Alignment = widget.ButtonAlignLeading
	o.continueCountry = widget.NewButton("Continue", func() {
		o.startCountrySetup(o.selected.Code)
	})
	o.continueCountry.Importance = widget.HighImportance
	o.continueCountry.Disable()
	heading := onboardingHeading("Choose your country")
	description := widget.NewLabel("Select a country to discover local radio stations.")
	description.Wrapping = fyne.TextWrapWord
	top := container.NewVBox(
		back,
		heading,
		description,
		o.search,
		widget.NewSeparator(),
	)
	bottom := container.NewPadded(o.continueCountry)
	o.window.setContent(container.NewPadded(container.NewBorder(top, bottom, nil, nil, o.list)))
}

// showWelcomeScreen returns from country selection to the system suggestion.
func (o *onboarding) showWelcomeScreen() {
	o.window.setContent(o.welcomeScreen())
}

// setCountries stores the directory response and updates the welcome suggestion.
func (o *onboarding) setCountries(countries []radiobrowser.Country, err error) {
	if o.window.onboarding != o {
		return
	}
	if o.countryActivity != nil {
		o.countryActivity.Stop()
		o.countryActivity = nil
	}
	if err != nil {
		if o.fromSettings {
			o.window.onboarding = nil
			o.window.setContent(o.window.appContent)
			dialog.ShowError(err, o.window.window)
			return
		}
		if o.choiceLabel != nil {
			if o.suggestedCode == "" {
				o.choiceLabel.Text = "Any country"
			} else {
				o.choiceLabel.Text = o.suggestedCode
			}
			o.choiceLabel.Refresh()
		}
		return
	}
	o.countries = make([]Country, 0, len(countries))
	for _, source := range countries {
		country := countryFromRadioBrowser(source)
		if country.Name != "" {
			o.countries = append(o.countries, country)
		}
	}
	if o.fromSettings {
		o.countries = append(o.countries, Country{Name: "Any country"})
	}
	sort.Slice(o.countries, func(i, j int) bool {
		if o.countries[i].Code == "" {
			return true
		}
		if o.countries[j].Code == "" {
			return false
		}
		return o.countries[i].Name < o.countries[j].Name
	})
	if o.fromSettings {
		o.showCountryPicker()
		return
	}
	for _, country := range o.countries {
		if strings.EqualFold(country.Code, o.suggestedCode) {
			o.suggested = country
			if o.choiceLabel != nil {
				o.choiceLabel.Text = country.Name
				o.choiceLabel.Refresh()
			}
			break
		}
	}
	if o.chooseCountry != nil {
		o.chooseCountry.Enable()
	}
}

// startCountrySetup loads stations before completing the initial country setup.
func (o *onboarding) startCountrySetup(code string) {
	w := o.window
	if o.fromSettings {
		if code == "" {
			w.settings.DiscoveryCountry = "*"
		} else {
			w.settings.DiscoveryCountry = "@" + strings.ToUpper(code)
		}
		if err := w.store.SaveSettings(w.settings); err != nil {
			dialog.ShowError(err, w.window)
			return
		}
		w.onboarding = nil
		w.setContent(w.appContent)
		w.refreshSettingsCountry()
		if w.home != nil {
			w.home.subtitle.SetText(homeSubtitle(w.settings.DiscoveryCountry))
			w.home.loadPopular()
		}
		return
	}
	o.pendingCode = strings.ToUpper(code)
	o.showCountryLoading()
	if w.directory == nil {
		o.finishCountrySetup(nil, errors.New("station directory unavailable"))
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), countrySetupTimeout)
		defer cancel()
		query := radiobrowser.Query{Limit: 20}
		if code != "" {
			query.CountryCode = strings.ToUpper(code)
		}
		stations, err := w.directory.Search(ctx, query)
		fyne.Do(func() { o.finishCountrySetup(stations, err) })
	}()
}

// showCountryLoading displays the final onboarding progress screen.
func (o *onboarding) showCountryLoading() {
	name := o.loadingCountryName()
	o.loadActivity = widget.NewActivity()
	o.loadDescription = loadingCountryMessage("Loading the best radio stations\nfor ", name, "...")
	o.loadError = widget.NewLabel("")
	o.loadError.Alignment = fyne.TextAlignCenter
	o.loadError.Wrapping = fyne.TextWrapWord
	o.loadError.Hide()
	connectStep, connectRow := newOnboardingStep(theme.RadioButtonFillIcon(), "Connecting to Radio Browser", brandAccentColorName)
	fetchStep, fetchRow := newOnboardingStep(theme.RadioButtonIcon(), "Fetching "+name+" stations", theme.ColorNamePlaceHolder)
	prepareStep, prepareRow := newOnboardingStep(theme.RadioButtonIcon(), "Preparing your radio experience", theme.ColorNamePlaceHolder)
	o.loadSteps = []*onboardingStep{connectStep, fetchStep, prepareStep}
	o.loadContinue = widget.NewButton("Continue", func() { o.window.showApp() })
	o.loadContinue.Importance = widget.HighImportance
	o.loadContinueBox = container.NewCenter(container.NewGridWrap(fyne.NewSize(400, 52), o.loadContinue))
	o.loadContinueBox.Hide()
	o.loadActivityBox = container.NewCenter(container.NewGridWrap(fyne.NewSize(64, 64), o.loadActivity))
	note := widget.NewLabel("This could take a few seconds.\nYou can always change country later in Settings.")
	note.Wrapping = fyne.TextWrapWord
	noteContent := container.NewBorder(
		nil, nil,
		container.NewPadded(widget.NewIcon(icons.Info())),
		nil,
		note,
	)
	noteBackground := canvas.NewRectangle(theme.Color(theme.ColorNameInputBackground))
	noteBackground.CornerRadius = theme.Size(theme.SizeNameCardRadius)
	noteBox := container.NewStack(noteBackground, noteContent)
	o.loadContent = container.NewVBox(
		verticalSpace(80),
		container.NewCenter(o.logo()),
		verticalSpace(24),
		container.NewCenter(onboardingHeading("Almost ready")),
		verticalSpace(8),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(400, 56), o.loadDescription)),
		verticalSpace(24),
		o.loadActivityBox,
		verticalSpace(20),
		container.NewCenter(container.NewVBox(connectRow, fetchRow, prepareRow)),
		container.NewCenter(o.loadError),
		verticalSpace(24),
		o.loadContinueBox,
		verticalSpace(48),
		noteBox,
	)
	o.window.setContent(container.NewCenter(container.NewPadded(o.loadContent)))
	o.loadActivity.Start()
}

// finishCountrySetup saves a successful setup or offers a retry without changing settings.
func (o *onboarding) finishCountrySetup(stations []radio.Station, err error) {
	w := o.window
	if w.onboarding != o {
		return
	}
	if err == nil {
		settings := w.settings
		settings.DiscoveryCountry = "*"
		if o.pendingCode != "" {
			settings.DiscoveryCountry = "@" + o.pendingCode
		}
		err = w.store.SaveSettings(settings)
		if err == nil {
			w.settings = settings
			// An empty successful response is ready too; nil means setup has not completed.
			if stations == nil {
				stations = []radio.Station{}
			}
			w.initialStations = stations
		}
	}
	if o.loadActivity != nil {
		o.loadActivity.Stop()
	}
	if o.loadActivityBox != nil {
		o.loadActivityBox.Hide()
	}
	if err != nil {
		if len(o.loadSteps) > 0 {
			o.loadSteps[0].set(theme.ErrorIcon(), "Setup failed", theme.ColorNameError)
		}
		if o.loadError != nil {
			o.loadError.SetText("Could not complete setup. Please try again.")
			o.loadError.Show()
		}
		if o.loadDescription != nil {
			o.loadDescription.Segments = loadingCountryMessage("Could not complete setup for ", o.loadingCountryName(), ".").Segments
			o.loadDescription.Refresh()
		}
		if o.loadContinue != nil {
			o.loadContinue.SetText("Retry")
			o.loadContinue.OnTapped = func() { o.startCountrySetup(o.pendingCode) }
		}
	} else {
		labels := []string{"Connected to Radio Browser", "Loaded stations for " + o.loadingCountryName(), "Radio experience ready"}
		for i, step := range o.loadSteps {
			if i < len(labels) {
				step.set(theme.ConfirmIcon(), labels[i], brandAccentColorName)
			}
		}
		if o.loadDescription != nil {
			o.loadDescription.Segments = loadingCountryMessage("Stations are ready for ", o.loadingCountryName(), ".").Segments
			o.loadDescription.Refresh()
		}
	}
	if o.loadContinueBox != nil {
		o.loadContinueBox.Show()
	}
	if o.loadContent != nil {
		o.loadContent.Refresh()
	}
}

// logo returns the FyneTune icon used across onboarding screens.
func (o *onboarding) logo() fyne.CanvasObject {
	icon := canvas.NewImageFromResource(fyne.NewStaticResource("FyneTune-icon.svg", iconSVG))
	icon.FillMode = canvas.ImageFillContain
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(64, 64), icon))
}

// welcomeTitle pairs the welcome text with FyneTune's wordmark asset.
func (o *onboarding) welcomeTitle() fyne.CanvasObject {
	wordmarkImage := canvas.NewImageFromResource(fyne.NewStaticResource("FyneTune-wordmark.svg", wordmarkSVG))
	wordmarkImage.FillMode = canvas.ImageFillContain
	wordmarkImage.SetMinSize(fyne.NewSize(112, 36))
	wordmark := container.NewBorder(
		container.NewGridWrap(fyne.NewSize(1, theme.Size(theme.SizeNamePadding)/2), layout.NewSpacer()),
		nil, nil, nil, wordmarkImage,
	)
	text := canvas.NewText("Welcome to", theme.Color(theme.ColorNameForeground))
	text.TextSize = theme.Size(theme.SizeNameHeadingText)
	text.TextStyle.Bold = true
	return container.NewCenter(container.NewHBox(
		text,
		wordmark,
	))
}

// onboardingHeading renders a large heading using the active Fyne theme.
func onboardingHeading(text string) fyne.CanvasObject {
	heading := canvas.NewText(text, theme.Color(theme.ColorNameForeground))
	heading.TextSize = theme.Size(theme.SizeNameHeadingText)
	heading.TextStyle.Bold = true
	return heading
}

// mutedLabel uses the active theme's placeholder color for supporting text.
func mutedLabel(text string) fyne.CanvasObject {
	label := canvas.NewText(text, theme.Color(theme.ColorNamePlaceHolder))
	label.TextSize = theme.Size(theme.SizeNameText)
	return label
}

// verticalSpace adds a fixed vertical gap while preserving Fyne's normal layout.
func verticalSpace(height float32) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(1, height), layout.NewSpacer())
}

// newOnboardingStep creates a loading status row with a themed icon and label.
func newOnboardingStep(icon fyne.Resource, text string, colorName fyne.ThemeColorName) (*onboardingStep, fyne.CanvasObject) {
	statusIcon := widget.NewIcon(theme.NewColoredResource(icon, colorName))
	label := widget.NewLabel(text)
	return &onboardingStep{icon: statusIcon, label: label}, container.NewHBox(statusIcon, label)
}

// loadingCountryMessage builds a centered message with the country name in bold.
func loadingCountryMessage(before, country, after string) *widget.RichText {
	regular := widget.RichTextStyleInline
	regular.Alignment = fyne.TextAlignCenter
	regular.SizeName = theme.SizeNameSubHeadingText
	strong := regular
	strong.TextStyle.Bold = true
	message := widget.NewRichText(
		&widget.TextSegment{Style: regular, Text: before},
		&widget.TextSegment{Style: strong, Text: country},
		&widget.TextSegment{Style: regular, Text: after},
	)
	message.Wrapping = fyne.TextWrapWord
	return message
}

// loadingCountryName returns the selected country or a generic regional label.
func (o *onboarding) loadingCountryName() string {
	if o.selected.Name != "" {
		return o.selected.Name
	}
	if o.suggested.Name != "" {
		return o.suggested.Name
	}
	return "your region"
}

// countryFlag converts a two-letter country code to its regional indicator flag.
func countryFlag(code string) string {
	code = strings.ToUpper(code)
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return "🌐"
	}
	return string([]rune{
		rune(0x1F1E6) + rune(code[0]-'A'),
		rune(0x1F1E6) + rune(code[1]-'A'),
	})
}

// suggestedFlag creates the enlarged country flag emoji for the welcome view.
func suggestedFlag(code string) fyne.CanvasObject {
	flag := canvas.NewText(countryFlag(code), theme.Color(theme.ColorNameForeground))
	flag.TextSize = theme.Size(theme.SizeNameHeadingText) * 2
	return flag
}

// systemLocaleCountryCode reads a two-letter region from common locale variables.
func systemLocaleCountryCode() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		locale := strings.TrimSpace(os.Getenv(key))
		if locale == "" {
			continue
		}
		locale, _, _ = strings.Cut(locale, ".")
		locale, _, _ = strings.Cut(locale, "@")
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
