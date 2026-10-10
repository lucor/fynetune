package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

const (
	radioStationLimit   = 20
	radioRequestLimit   = 15 * time.Second
	radioCacheTTL       = 24 * time.Hour
	startupSpinnerDelay = 300 * time.Millisecond
)

// showStartup displays the loading view until cached or fetched stations are ready.
// One goroutine handles the cache read and any refresh. The view offers Retry when
// no cached data is available, and prevents navigation until loading completes.
func (w *Window) showStartup() {
	store := cache.New(w.app.Cache())
	query, popular := radioStationQuery(w.settings.DiscoveryCountry)
	key := radioStationCacheKey(query, popular)
	logo := canvas.NewImageFromResource(homeBrandIcon)
	logo.FillMode = canvas.ImageFillContain
	wordmark := canvas.NewImageFromResource(fyne.NewStaticResource("FyneTune-wordmark.svg", wordmarkSVG))
	wordmark.FillMode = canvas.ImageFillContain
	activity := widget.NewActivity()
	activity.Hide()
	status := widget.NewLabel("")
	status.Alignment = fyne.TextAlignCenter
	status.Hide()
	retry := widget.NewButton("Retry", nil)
	retry.Importance = widget.HighImportance
	retry.Hide()
	w.setContent(container.NewCenter(container.NewVBox(
		container.NewCenter(container.NewGridWrap(fyne.NewSize(80, 80), logo)),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(180, 59), wordmark)),
		container.NewCenter(container.NewGridWrap(fyne.NewSize(24, 24), activity)),
		status,
		container.NewCenter(retry),
	)))
	load := func() {
		retry.Hide()
		status.Hide()
		activity.Hide()
		finished := false
		// Fast cache reads need no spinner. Keep its space reserved to avoid moving the logos.
		spinnerTimer := time.AfterFunc(startupSpinnerDelay, func() {
			fyne.Do(func() {
				if !finished {
					activity.Show()
					activity.Start()
				}
			})
		})
		go func() {
			var stations []radio.Station
			result, err := store.Get(key, radioCacheTTL, &stations)
			if err != nil {
				slog.Warn("could not read cached radio stations", "error", err)
				result = cache.Result{}
				stations = nil
			}
			if !result.Fresh {
				cached := stations
				ctx, cancel := context.WithTimeout(context.Background(), radioRequestLimit)
				defer cancel()
				switch {
				case w.directory == nil:
					err = errors.New("station directory unavailable")
				case popular:
					stations, err = w.directory.Popular(ctx, query.Limit)
				default:
					stations, err = w.directory.Search(ctx, query)
				}
				if err == nil {
					if cacheErr := store.Set(key, stations); cacheErr != nil {
						slog.Warn("could not cache radio stations", "error", cacheErr)
					}
				} else if result.Found {
					slog.Debug("could not refresh cached radio stations", "error", err)
					stations, err = cached, nil
				}
			}
			fyne.Do(func() {
				finished = true
				spinnerTimer.Stop()
				activity.Stop()
				activity.Hide()
				if err != nil {
					status.SetText("Could not load stations.\nCheck your connection and try again.")
					status.Show()
					retry.Show()
					return
				}
				w.home.setStations(stations)
				w.setContent(w.appContent)
			})
		}()
	}
	retry.OnTapped = load
	load()
}

// radioStationQuery selects global popular stations or the saved country's stations.
func radioStationQuery(region string) (radiobrowser.Query, bool) {
	query := radiobrowser.Query{Limit: radioStationLimit}
	switch {
	case region == "", region == "*":
		return query, true
	case strings.HasPrefix(region, "@"):
		query.CountryCode = strings.TrimPrefix(region, "@")
	default:
		query.Country = region
		query.CountryExact = true
	}
	return query, false
}

// radioStationCacheKey identifies the query and preserves existing cache filenames.
func radioStationCacheKey(query radiobrowser.Query, popular bool) string {
	return fmt.Sprintf("home-stations:%t:%q:%q:%t:%d", popular, query.Country, query.CountryCode, query.CountryExact, query.Limit)
}
