package ui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

const (
	homeStationLimit = 20
	homeRequestLimit = 15 * time.Second
)

// homeSection loads the directory stations shown on the Radio tab.
type homeSection struct {
	*fyne.Container
	window    *Window
	subtitle  *widget.Label
	status    *widget.Label
	rows      *stationList
	requestID uint64
}

// newHomeSection creates the Radio section and its reusable station list.
func newHomeSection(w *Window) *homeSection {
	h := &homeSection{
		window:   w,
		subtitle: widget.NewLabel(homeSubtitle(w.settings.DiscoveryCountry)),
		status:   widget.NewLabel("Loading stations…"),
		rows:     newStationList(w, false),
	}
	h.subtitle.SizeName = theme.SizeNameText
	h.Container = container.NewBorder(h.status, nil, nil, nil, h.rows.list)
	return h
}

// setStations replaces the displayed directory results.
func (h *homeSection) setStations(stations []radio.Station) {
	h.requestID++
	h.rows.setStations(stations)
	if len(stations) == 0 {
		h.setStatus("No stations found.")
	} else {
		h.setStatus("")
	}
}

// homeSubtitle describes the popular stations shown for the saved country.
func homeSubtitle(region string) string {
	region = strings.TrimSpace(region)
	switch {
	case region == "", region == "*":
		return "Popular stations"
	case strings.HasPrefix(region, "@"):
		if name := countryNameForCode(strings.TrimPrefix(region, "@")); name != "" {
			return "Popular stations in " + name
		}
		return "Popular stations"
	default:
		return "Popular stations in " + region
	}
}

// setStatus shows loading and empty-result messages above the station list.
func (h *homeSection) setStatus(text string) {
	h.status.SetText(text)
	if text == "" {
		h.status.Hide()
	} else {
		h.status.Show()
	}
	h.Refresh()
}

// loadPopular requests stations for the saved discovery region.
func (h *homeSection) loadPopular() {
	query := radiobrowser.Query{Limit: homeStationLimit}
	popular := true
	region := h.window.settings.DiscoveryCountry
	switch {
	case region == "", region == "*":
	case strings.HasPrefix(region, "@"):
		query.CountryCode = strings.TrimPrefix(region, "@")
		popular = false
	default:
		query.Country = region
		query.CountryExact = true
		popular = false
	}
	h.fetch(query, popular, "Loading stations…")
}

// fetch loads matching stations and applies a result only while it is current.
func (h *homeSection) fetch(query radiobrowser.Query, popular bool, status string) {
	h.requestID++
	requestID := h.requestID
	h.setStatus(status)
	if h.window.directory == nil {
		h.setStatus("Station directory unavailable.")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), homeRequestLimit)
		defer cancel()
		var stations []radio.Station
		var err error
		if popular {
			stations, err = h.window.directory.Popular(ctx, query.Limit)
		} else {
			stations, err = h.window.directory.Search(ctx, query)
		}
		fyne.Do(func() {
			if requestID != h.requestID {
				return
			}
			if err != nil {
				h.setStatus("Could not load stations. Check your connection and try again.")
				h.rows.setStations(nil)
				return
			}
			h.setStations(stations)
		})
	}()
}

// homeStationDetails formats the station description, codec, and bitrate.
func homeStationDetails(station radio.Station) (description, codec, bitrate string) {
	parts := make([]string, 0, 2)
	if len(station.Tags) > 0 && strings.TrimSpace(station.Tags[0]) != "" {
		parts = append(parts, strings.TrimSpace(station.Tags[0]))
	}
	if country := stationCountryName(station); country != "" {
		parts = append(parts, country)
	}
	if len(parts) == 0 {
		parts = append(parts, homeStationHost(station.URL))
	}
	description = strings.Join(parts, " · ")
	codec = strings.TrimSpace(station.Codec)
	if codec != "" && !strings.EqualFold(codec, "unknown") {
		codec = strings.ToUpper(codec)
	} else {
		codec = ""
	}
	if station.Bitrate > 0 {
		bitrate = fmt.Sprintf("%d kbps", station.Bitrate)
	}
	return description, codec, bitrate
}

// homeStationHost returns the stream host when station metadata is unavailable.
func homeStationHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "Internet radio"
	}
	return parsed.Hostname()
}
