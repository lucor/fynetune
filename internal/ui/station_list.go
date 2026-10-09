package ui

import (
	"context"
	"fmt"
	"image"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/hashicorp/golang-lru/v2"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
	"go.lucor.dev/fynetune/internal/ui/widgets"
	xdraw "golang.org/x/image/draw"
)

const (
	stationIconSize     = 256
	stationArtworkSize  = 52
	stationArtworkRound = 8
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
	iconCache      *lru.Cache[string, favicon.Icon]
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
	recentName   *widget.Label
	recentTime   *widget.Label
}

// newStationList builds the shared station list for Home, Favorites, or Recent.
func newStationList(w *Window, showPlayedAt bool) *stationList {
	if w.iconCache == nil {
		w.iconCache = newHomeStationIconCache()
	}
	s := &stationList{
		window:       w,
		showPlayedAt: showPlayedAt,
		rows:         make(map[fyne.CanvasObject]*stationListRow),
		iconClient:   &http.Client{Timeout: 8 * time.Second},
		iconCache:    w.iconCache,
		iconLoading:  make(map[string]struct{}),
		iconFailed:   make(map[string]struct{}),
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

// setRecent replaces the list with stations and their last played times.
func (s *stationList) setRecent(recent []storage.RecentStation) {
	s.iconGeneration++
	s.iconLoading = make(map[string]struct{})
	s.iconFailed = make(map[string]struct{})
	s.entries = make([]stationListEntry, 0, len(recent))
	for _, item := range recent {
		s.entries = append(s.entries, stationListEntry{station: item.Station, playedAt: item.PlayedAt})
	}
	s.list.Refresh()
}

// newRow creates one row with shared artwork and favorite controls.
func (s *stationList) newRow() fyne.CanvasObject {
	row := &stationListRow{artwork: newStationArtwork()}
	row.favorite = widget.NewButtonWithIcon("", homeFavoriteOutline, func() {
		s.window.toggleFavorite(row.station)
	})
	row.favorite.Importance = widget.LowImportance
	if s.showPlayedAt {
		row.recentName = widget.NewLabelWithStyle("Station", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
		row.recentName.Truncation = fyne.TextTruncateEllipsis
		row.recentTime = widget.NewLabelWithStyle("Today, 10:24", fyne.TextAlignTrailing, fyne.TextStyle{})
		row.recentTime.Importance = widget.LowImportance
		content := container.NewBorder(nil, nil, nil, row.recentTime, row.recentName)
		return s.wrapRow(row, content)
	}
	row.title = widget.NewLabelWithStyle("Station", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	row.title.Truncation = fyne.TextTruncateEllipsis
	row.description = widget.NewLabel("Station details")
	row.description.Importance = widget.LowImportance
	row.description.SizeName = stationMetadataSizeName
	row.description.Truncation = fyne.TextTruncateEllipsis
	row.codecBadge = widgets.NewBadge("MP3", theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
	row.codecBadge.TextSize = theme.Size(theme.SizeNameCaptionText)
	row.codecBadge.PaddingX = 8
	row.codecBadge.PaddingY = 2
	row.bitrateBadge = widgets.NewBadge("128 kbps", theme.Color(stationBadgeColorName), theme.Color(stationMetadataColorName))
	row.bitrateBadge.TextSize = theme.Size(theme.SizeNameCaptionText)
	row.bitrateBadge.PaddingX = 8
	row.bitrateBadge.PaddingY = 2
	content := container.NewPadded(container.NewVBox(row.title, row.description, container.NewHBox(row.codecBadge, row.bitrateBadge)))
	return s.wrapRow(row, container.NewThemeOverride(content, compactTextTheme{Theme: s.window.app.Settings().Theme()}))
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
	if s.showPlayedAt {
		item = container.NewVBox(item, widget.NewSeparator())
	}
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
	description, codec, bitrate := homeStationDetails(station)
	if s.showPlayedAt {
		row.recentName.SetText(station.Name)
		row.recentTime.SetText(recentTimestamp(entry.playedAt))
	} else {
		row.title.SetText(station.Name)
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
	favoriteIcon := homeFavoriteOutline
	if s.window.isFavorite(station.ID) {
		favoriteIcon = homeFavoriteFilled
	}
	row.favorite.SetIcon(favoriteIcon)
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

// setStationArtwork applies a cached favicon or starts one bounded async fetch.
func (s *stationList) setStationArtwork(row *stationListRow, station radio.Station) {
	key := stationIconKey(station)
	applyStationIcon(row.artwork, favicon.Icon{SVG: homeBrandIcon.Content(), Format: "svg"})
	if key == "" {
		return
	}
	if icon, ok := s.iconCache.Get(key); ok {
		applyStationIcon(row.artwork, icon)
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

// setPlayerArtwork applies the selected station's cached favicon when available.
func (s *stationList) setPlayerArtwork(station radio.Station) {
	if s.window.playerBar == nil {
		return
	}
	applyStationIcon(s.window.playerBar.artwork, favicon.Icon{SVG: homeBrandIcon.Content(), Format: "svg"})
	if icon, ok := s.iconCache.Get(stationIconKey(station)); ok {
		applyStationIcon(s.window.playerBar.artwork, icon)
	}
}

// fetchStationArtwork downloads the homepage icon, then the Radio Browser URL.
func (s *stationList) fetchStationArtwork(row *stationListRow, station radio.Station, key string, generation uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var icon favicon.Icon
	var err error
	if homepage := strings.TrimSpace(station.Homepage); homepage != "" {
		if parsed, parseErr := url.Parse(homepage); parseErr == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") {
			icon, err = favicon.Download(ctx, parsed, favicon.Options{Client: s.iconClient})
		}
	}
	if err != nil || (icon.Raster == nil && len(icon.SVG) == 0) {
		if faviconURL := strings.TrimSpace(station.Favicon); faviconURL != "" {
			icon, err = favicon.DownloadURL(ctx, faviconURL, favicon.Options{Client: s.iconClient})
		}
	}
	if err == nil {
		if icon.Raster == nil && len(icon.SVG) == 0 {
			err = fmt.Errorf("favicon contains no image")
		} else if icon.Raster != nil {
			icon.Raster = resizeStationIcon(icon.Raster)
		}
	}
	fyne.Do(func() {
		if generation != s.iconGeneration {
			return
		}
		delete(s.iconLoading, key)
		if err != nil {
			s.iconFailed[key] = struct{}{}
			return
		}
		s.iconCache.Add(key, icon)
		if stationIconKey(row.station) == key {
			applyStationIcon(row.artwork, icon)
		}
		if stationIconKey(s.window.selectedStation) == key && s.window.playerBar != nil {
			applyStationIcon(s.window.playerBar.artwork, icon)
		}
	})
}

// newHomeStationIconCache shares a bounded icon cache across station sections.
func newHomeStationIconCache() *lru.Cache[string, favicon.Icon] {
	cache, err := lru.New[string, favicon.Icon](64)
	if err != nil {
		panic("station icon cache capacity must be positive: " + err.Error())
	}
	return cache
}

// stationIconKey identifies a station icon source independent of row reuse.
func stationIconKey(station radio.Station) string {
	faviconURL := strings.TrimSpace(station.Favicon)
	homepageURL := strings.TrimSpace(station.Homepage)
	if faviconURL == "" && homepageURL == "" {
		return ""
	}
	return homepageURL + "\x00" + faviconURL
}

// resizeStationIcon bounds decoded raster memory while retaining display detail.
func resizeStationIcon(source image.Image) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= stationIconSize && height <= stationIconSize {
		return source
	}
	if width >= height {
		height = max(1, height*stationIconSize/width)
		width = stationIconSize
	} else {
		width = max(1, width*stationIconSize/height)
		height = stationIconSize
	}
	resized := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(resized, resized.Bounds(), source, bounds, xdraw.Src, nil)
	return resized
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
