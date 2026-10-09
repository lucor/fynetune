package ui

import (
	_ "embed"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/ui/navigation"
)

//go:embed assets/logo.png
var logoPNG []byte

//go:embed assets/icon.svg
var iconSVG []byte

//go:embed assets/wordmark.svg
var wordmarkSVG []byte

//go:embed assets/favorite-outline.png
var favoriteOutlinePNG []byte

//go:embed assets/favorite-filled.png
var favoriteFilledPNG []byte

var (
	homeBrandIcon       = fyne.NewStaticResource("FyneTune-icon.svg", iconSVG)
	homeFavoriteOutline = fyne.NewStaticResource("favorite-outline.png", favoriteOutlinePNG)
	homeFavoriteFilled  = fyne.NewStaticResource("favorite-filled.png", favoriteFilledPNG)
)

// newApp builds the app view with its section header and bottom navigation.
func newApp(w *Window) fyne.CanvasObject {
	home := newHomeSection(w)
	w.home = home
	favorites := newFavoritesSection(w)
	w.favorites = favorites
	recent := newRecentSection(w)
	w.recentSection = recent
	explore := newExploreSection(w)
	pages := []fyne.CanvasObject{
		home.Container,
		favorites.Container,
		recent.Container,
		explore.Container,
		w.settingsPage(),
	}
	content := container.NewStack(pages[0])
	w.playerBar = newPlayerWidget(w.app.Settings().Theme(), w.settings.Volume, w.togglePlayback, w.setVolume, w.toggleMute)

	headerText := widget.NewLabelWithStyle("Radio", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	w.sectionHeader = headerText
	headerText.SizeName = theme.SizeNameHeadingText
	header := container.NewVBox(headerText, home.subtitle)

	nav := navigation.New(
		navigation.Item{
			Icon:     homeBrandIcon,
			IconSize: theme.Size(theme.SizeNameInlineIcon) + theme.Size(theme.SizeNamePadding),
		},
		navigation.Item{
			Icon:         homeFavoriteOutline,
			SelectedIcon: homeFavoriteFilled,
		},
		navigation.Item{Icon: theme.HistoryIcon()},
		navigation.Item{Icon: theme.SearchIcon()},
		navigation.Item{Icon: theme.SettingsIcon()},
	)
	headerTitles := []string{"Radio", "Favorites", "Recent", "Explore", "Settings"}
	nav.OnSelected = func(index int) {
		content.Objects = []fyne.CanvasObject{pages[index]}
		content.Refresh()
		headerText.SetText(headerTitles[index])
		if index == 0 {
			home.subtitle.Show()
		} else {
			home.subtitle.Hide()
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
