package ui

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
)

// homeSection displays the stations loaded before opening the Radio tab.
type homeSection struct {
	*fyne.Container
	subtitle *widget.Label
	status   *widget.Label
	rows     *stationList
}

// newHomeSection creates the Radio section and its reusable station list.
func newHomeSection(w *Window) *homeSection {
	h := &homeSection{
		subtitle: widget.NewLabel(homeSubtitle(w.settings.DiscoveryCountry)),
		status:   widget.NewLabel("Loading stations…"),
		rows:     newStationList(w, false),
	}
	h.subtitle.SizeName = theme.SizeNameText
	h.Container = container.NewBorder(h.status, nil, nil, nil, h.rows.list)
	return h
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

// setStations replaces the displayed stations supplied by onboarding.
func (h *homeSection) setStations(stations []radio.Station) {
	h.rows.setStations(stations)
	if len(stations) == 0 {
		h.setStatus("No stations found.")
	} else {
		h.setStatus("")
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
