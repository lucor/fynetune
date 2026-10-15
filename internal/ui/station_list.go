package ui

import (
	"context"
	"net/http"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/ui/assets/icons"
	"go.lucor.dev/fynetune/internal/ui/widgets"
)

const (
	stationArtworkSize   = 52
	stationArtworkRound  = 8
	stationBadgePaddingY = 2
)

// stationListEntry pairs a station with optional Recent playback time.
type stationListEntry struct {
	station  radio.Station
	playedAt time.Time
}

// stationList displays stations with shared artwork, favorite, and play behavior.
type stationList struct {
	window         *Window
	showPlayedAt   bool
	list           *widget.List
	entries        []stationListEntry
	rows           map[fyne.CanvasObject]*stationListRow
	iconClient     *http.Client
	iconLoading    map[string]struct{}
	iconFailed     map[string]struct{}
	iconGeneration uint64
}

// stationListRow contains the reusable widgets for one station.
type stationListRow struct {
	station      radio.Station
	artwork      *canvas.Image
	favorite     *widget.Button
	title        *widget.Label
	description  *widget.Label
	codecBadge   *widgets.Badge
	bitrateBadge *widgets.Badge
}

// newStationList builds the shared station list for Home, Favorites, or Recent.
func newStationList(w *Window, showPlayedAt bool) *stationList {
	s := &stationList{
		window:       w,
		showPlayedAt: showPlayedAt,
		rows:         make(map[fyne.CanvasObject]*stationListRow),
		iconClient:   &http.Client{Timeout: 8 * time.Second},
		iconLoading:  make(map[string]struct{}),
		iconFailed:   make(map[string]struct{}),
	}
	if showPlayedAt {
		// Recent composes its bounded history in one Scroll, keeping day labels
		// outside interactive controls instead of representing them as list items.
		return s
	}
	s.list = widget.NewList(
		func() int { return len(s.entries) },
		s.newRow,
		s.updateRow,
	)
	s.list.HideSeparators = true
	s.list.OnSelected = func(id widget.ListItemID) {
		if int(id) >= len(s.entries) {
			return
		}
		station := s.entries[id].station
		s.setPlayerArtwork(station)
		s.window.selectStation(station)
	}
	return s
}

// setStations replaces the list with ordinary station details.
func (s *stationList) setStations(stations []radio.Station) {
	s.iconGeneration++
	s.iconLoading = make(map[string]struct{})
	s.iconFailed = make(map[string]struct{})
	s.entries = make([]stationListEntry, 0, len(stations))
	for _, station := range stations {
		s.entries = append(s.entries, stationListEntry{station: station})
	}
	s.list.Refresh()
}

// setRecent sorts global history newest first and clears the previous row objects.
func (s *stationList) setRecent(recent []storage.RecentStation) {
	s.iconGeneration++
	s.iconLoading = make(map[string]struct{})
	s.iconFailed = make(map[string]struct{})
	items := slices.Clone(recent)
	slices.SortStableFunc(items, func(a, b storage.RecentStation) int { return b.PlayedAt.Compare(a.PlayedAt) })
	s.rows = make(map[fyne.CanvasObject]*stationListRow)
	s.entries = make([]stationListEntry, 0, len(items))
	for _, item := range items {
		s.entries = append(s.entries, stationListEntry{station: item.Station, playedAt: item.PlayedAt})
	}
}

// newRow creates one row with shared artwork and favorite controls.
func (s *stationList) newRow() fyne.CanvasObject {
	row := &stationListRow{artwork: newStationArtwork()}
	row.favorite = widget.NewButtonWithIcon("", icons.Favorites(), func() {
		s.window.toggleFavorite(row.station)
	})
	row.favorite.Importance = widget.LowImportance
	row.title = widget.NewLabelWithStyle("Station", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	row.title.Truncation = fyne.TextTruncateEllipsis
	row.description = widget.NewLabel("Station details")
	row.description.Importance = widget.LowImportance
	row.description.SizeName = stationMetadataSizeName
	row.description.Truncation = fyne.TextTruncateEllipsis
	content := container.NewVBox(row.title, row.description)
	if s.showPlayedAt {
		row.description.SetText("14:37")
		padding := theme.Size(theme.SizeNamePadding)
		// Distribute the missing badge line above and below the text, keeping
		// Recent rows the same height as Radio and Favorites without extra content.
		badgeHeight := fyne.MeasureText("MP3", theme.Size(theme.SizeNameCaptionText), fyne.TextStyle{}).Height + 2*stationBadgePaddingY
		verticalPadding := padding + (badgeHeight+padding)/2
		content = container.New(layout.NewCustomPaddedLayout(verticalPadding, verticalPadding, padding, padding), content)
		return s.wrapRow(row, container.NewThemeOverride(content, compactTextTheme{Theme: s.window.app.Settings().Theme()}))
	}
	row.codecBadge = widgets.NewBadge("MP3", theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
	row.codecBadge.TextSize = theme.Size(theme.SizeNameCaptionText)
	row.codecBadge.PaddingX = 8
	row.codecBadge.PaddingY = stationBadgePaddingY
	row.bitrateBadge = widgets.NewBadge("128 kbps", theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
	row.bitrateBadge.TextSize = theme.Size(theme.SizeNameCaptionText)
	row.bitrateBadge.PaddingX = 8
	row.bitrateBadge.PaddingY = stationBadgePaddingY
	content.Add(container.NewHBox(row.codecBadge, row.bitrateBadge))
	return s.wrapRow(row, container.NewThemeOverride(container.NewPadded(content), compactTextTheme{Theme: s.window.app.Settings().Theme()}))
}

// wrapRow adds the common icon and favorite controls around section content.
func (s *stationList) wrapRow(row *stationListRow, content fyne.CanvasObject) fyne.CanvasObject {
	favorite := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(44), row.favorite))
	item := container.NewBorder(
		nil, nil,
		container.NewCenter(row.artwork),
		favorite,
		content,
	)
	s.rows[item] = row
	return item
}

// updateRow updates the reusable content and favorite state for one station.
func (s *stationList) updateRow(id widget.ListItemID, object fyne.CanvasObject) {
	if int(id) >= len(s.entries) {
		return
	}
	row := s.rows[object]
	entry := s.entries[id]
	station := entry.station
	row.station = station
	row.title.SetText(station.Name)
	if s.showPlayedAt {
		row.description.SetText(entry.playedAt.Local().Format("15:04"))
	} else {
		description, codec, bitrate := homeStationDetails(station)
		row.description.SetText(description)
		row.codecBadge.SetText(codec)
		row.bitrateBadge.SetText(bitrate)
		row.codecBadge.SetColors(theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
		row.bitrateBadge.SetColors(theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
		if codec == "" {
			row.codecBadge.Hide()
		} else {
			row.codecBadge.Show()
		}
		if bitrate == "" {
			row.bitrateBadge.Hide()
		} else {
			row.bitrateBadge.Show()
		}
	}
	s.setStationArtwork(row, station)
	s.setFavoriteIcon(row)
}

// refreshTheme updates favorite icons and badge colors in rows held by this list.
func (s *stationList) refreshTheme() {
	for _, row := range s.rows {
		s.setFavoriteIcon(row)
		if row.codecBadge != nil {
			row.codecBadge.SetColors(theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
			row.bitrateBadge.SetColors(theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
		}
	}
}

// refreshFavorites updates the favorite state of visible rows and rebuilds the list.
func (s *stationList) refreshFavorites() {
	for _, row := range s.rows {
		s.setFavoriteIcon(row)
	}
	if s.list != nil {
		s.list.Refresh()
	}
}

// setFavoriteIcon shows the theme-colored outline or blue selected heart.
func (s *stationList) setFavoriteIcon(row *stationListRow) {
	if s.window.isFavorite(row.station.ID) {
		row.favorite.SetIcon(icons.FavoritesActive())
		return
	}
	row.favorite.SetIcon(icons.Favorites())
}

// newStationArtwork displays the FyneTune icon until a station favicon loads.
func newStationArtwork() *canvas.Image {
	artwork := canvas.NewImageFromResource(homeBrandIcon)
	artwork.FillMode = canvas.ImageFillContain
	artwork.CornerRadius = stationArtworkRound
	size := fyne.NewSquareSize(stationArtworkSize)
	artwork.SetMinSize(size)
	artwork.Resize(size)
	return artwork
}

// setStationArtwork applies the placeholder or starts an asynchronous icon load.
func (s *stationList) setStationArtwork(row *stationListRow, station radio.Station) {
	key := stationIconKey(station)
	applyStationIcon(row.artwork, favicon.Icon{SVG: homeBrandIcon.Content(), Format: "svg"})
	if key == "" {
		return
	}
	if _, failed := s.iconFailed[key]; failed {
		return
	}
	if _, loading := s.iconLoading[key]; loading {
		return
	}
	s.iconLoading[key] = struct{}{}
	go s.fetchStationArtwork(row, station, key, s.iconGeneration)
}

// setPlayerArtwork applies the placeholder and loads the selected station's icon.
func (s *stationList) setPlayerArtwork(station radio.Station) {
	if s.window.playerBar == nil {
		return
	}
	applyStationIcon(s.window.playerBar.artwork, favicon.Icon{SVG: homeBrandIcon.Content(), Format: "svg"})
	key := stationIconKey(station)
	if key == "" {
		return
	}
	if _, loading := s.iconLoading[key]; loading {
		return
	}
	go s.fetchPlayerArtwork(station, key)
}

// fetchStationArtwork loads a persistent or downloaded icon outside the UI thread.
func (s *stationList) fetchStationArtwork(row *stationListRow, station radio.Station, key string, generation uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	icon, err := s.loadStationIcon(ctx, station, key)
	fyne.Do(func() {
		if generation != s.iconGeneration {
			return
		}
		delete(s.iconLoading, key)
		if err != nil {
			s.iconFailed[key] = struct{}{}
			return
		}
		if stationIconKey(row.station) == key {
			applyStationIcon(row.artwork, icon)
		}
		if stationIconKey(s.window.selectedStation) == key && s.window.playerBar != nil {
			applyStationIcon(s.window.playerBar.artwork, icon)
		}
	})
}

// fetchPlayerArtwork loads a station icon for the player from persistent cache or network.
func (s *stationList) fetchPlayerArtwork(station radio.Station, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	icon, err := s.loadStationIcon(ctx, station, key)
	if err != nil {
		return
	}
	fyne.Do(func() {
		if stationIconKey(s.window.selectedStation) == key && s.window.playerBar != nil {
			applyStationIcon(s.window.playerBar.artwork, icon)
		}
	})
}

// applyStationIcon applies a raster or vector favicon to an image widget.
func applyStationIcon(target *canvas.Image, icon favicon.Icon) {
	target.Image = icon.Raster
	target.Resource = nil
	if len(icon.SVG) > 0 {
		target.Resource = fyne.NewStaticResource("station-favicon.svg", icon.SVG)
	}
	target.Refresh()
}
