package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
	"go.lucor.dev/fynetune/internal/ui/navigation"
)

//go:embed assets/logo.png
var logoPNG []byte

//go:embed assets/icon.svg
var iconSVG []byte

//go:embed assets/wordmark.svg
var wordmarkSVG []byte

var homeBrandIcon = fyne.NewStaticResource("FyneTune-icon.svg", iconSVG)

// newApp builds the app view with its section header and bottom navigation.
func newApp(w *Window) fyne.CanvasObject {
	home := newHomeSection(w)
	w.home = home
	favorites := newFavoritesSection(w)
	w.favorites = favorites
	recent := newRecentSection(w)
	w.recentSection = recent
	discover := newDiscoverSection(w)
	pages := []fyne.CanvasObject{
		home.Container,
		favorites.Container,
		recent.Container,
		discover.Container,
		w.settingsPage(),
	}
	content := container.NewStack(pages[0])
	w.playerBar = newPlayerWidget(w, w.settings.Volume, w.togglePlayback, w.setVolume, w.toggleMute)

	headerText := widget.NewLabelWithStyle("Radio", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.sectionHeader = headerText
	headerText.SizeName = theme.SizeNameHeadingText
	sectionHeader := container.NewBorder(nil, nil, nil, favorites.saved, headerText)
	favorites.saved.Hide()
	favorites.subtitle.Hide()
	recent.subtitle.Hide()
	discover.subtitle.Hide()
	settingsSubtitle := widget.NewLabel("Customize your FyneTune experience")
	settingsSubtitle.Hide()
	header := container.NewThemeOverride(
		container.NewPadded(container.NewVBox(sectionHeader, home.subtitle, favorites.subtitle, recent.subtitle, discover.subtitle, settingsSubtitle)),
		compactTextTheme{Theme: w.app.Settings().Theme()},
	)

	nav := navigation.New(
		navigation.Item{
			Icon:     homeBrandIcon,
			IconSize: theme.Size(theme.SizeNameInlineIcon) + theme.Size(theme.SizeNamePadding),
		},
		navigation.Item{Icon: icons.Favorites(), SelectedIcon: icons.FavoritesActive()},
		navigation.Item{Icon: icons.Recent(), SelectedIcon: icons.RecentActive()},
		navigation.Item{Icon: icons.Discover(), SelectedIcon: icons.DiscoverActive()},
		navigation.Item{Icon: icons.Settings(), SelectedIcon: icons.SettingsActive()},
	)
	w.bottomNavigation = nav
	w.refreshIconTheme()
	headerTitles := []string{"Radio", "Favorites", "Recent", "Discover", "Settings"}
	nav.OnSelected = func(index int) {
		content.Objects = []fyne.CanvasObject{pages[index]}
		content.Refresh()
		headerText.SetText(headerTitles[index])
		if index == 0 {
			home.subtitle.Show()
		} else {
			home.subtitle.Hide()
		}
		if index == 1 {
			favorites.subtitle.Show()
		} else {
			favorites.subtitle.Hide()
		}
		if index == 1 {
			favorites.saved.Show()
		} else {
			favorites.saved.Hide()
		}
		if index == 2 {
			recent.subtitle.Show()
			recent.refresh()
		} else {
			recent.subtitle.Hide()
		}
		if index == 4 {
			settingsSubtitle.Show()
		} else {
			settingsSubtitle.Hide()
		}
		if index == 3 {
			discover.subtitle.Show()
		} else {
			discover.subtitle.Hide()
		}
		header.Refresh()
	}

	footer := container.NewVBox(
		container.NewPadded(w.playerBar.Container),
		widget.NewSeparator(),
		container.NewPadded(nav),
	)
	return container.NewBorder(container.NewVBox(header, widget.NewSeparator()), footer, nil, nil, content)
}

// placeholder marks a section's content until its screen is designed.
func placeholder(name string) fyne.CanvasObject {
	return container.NewCenter(widget.NewLabel(name))
}
