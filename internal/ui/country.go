package ui

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

const countryCacheTTL = 7 * 24 * time.Hour

const countryCacheKey = "radio-browser-countries"

// Country is the UI representation of a country, with a stable ISO code and display name.
type Country struct {
	Code string
	Name string
}

// loadCountries shares the persistent country list across the app's screens.
// The lock serializes cache reads and refreshes; a failed refresh preserves stale data.
// Callers provide a request deadline and run this method outside the UI thread.
func (w *Window) loadCountries(ctx context.Context) ([]radiobrowser.Country, error) {
	w.countriesMu.Lock()
	defer w.countriesMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cached, result := w.cachedCountries()
	if result.Fresh {
		return cached, nil
	}
	var countries []radiobrowser.Country
	var err error
	if w.directory == nil {
		err = errors.New("station directory unavailable")
	} else {
		countries, err = w.directory.Countries(ctx)
	}
	// Explicit cancellation aborts the action; a network timeout can still use stale data.
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, context.Canceled
	}
	if err != nil {
		if result.Found {
			slog.Debug("could not refresh cached countries", "error", err)
			return cached, nil
		}
		return nil, err
	}
	if err := cache.New(w.app.Cache()).Set(countryCacheKey, countries); err != nil {
		slog.Warn("could not cache countries", "error", err)
	}
	return countries, nil
}

// cachedCountries reads the local list; an unreadable record is treated as a cache miss.
func (w *Window) cachedCountries() ([]radiobrowser.Country, cache.Result) {
	var countries []radiobrowser.Country
	result, err := cache.New(w.app.Cache()).Get(countryCacheKey, countryCacheTTL, &countries)
	if err != nil {
		slog.Warn("could not read cached countries", "error", err)
		return nil, cache.Result{}
	}
	return countries, result
}

// discoveryCountry turns a saved region preference into a displayable country.
func discoveryCountry(region string) Country {
	region = strings.TrimSpace(region)
	switch {
	case region == "", region == "*":
		return Country{Name: "Any country"}
	case strings.HasPrefix(region, "@"):
		code := strings.ToUpper(strings.TrimPrefix(region, "@"))
		name := countryNameForCode(code)
		if name == "" {
			name = code
		}
		return Country{Code: code, Name: name}
	default:
		return Country{Name: region}
	}
}

// countryOptionFlag returns a flag or a globe for the any-country option.
func countryOptionFlag(code string) string {
	if strings.TrimSpace(code) == "" {
		return "🌐"
	}
	return countryFlag(code)
}

// countryFromRadioBrowser maps a directory record to its normalized UI country.
func countryFromRadioBrowser(source radiobrowser.Country) Country {
	code := strings.ToUpper(strings.TrimSpace(source.Code))
	name := strings.TrimSpace(source.Name)
	if displayName := countryNameForCode(code); displayName != "" {
		name = displayName
	}
	return Country{Code: code, Name: name}
}

// countriesFromRadioBrowser normalizes directory entries and removes duplicate ISO codes.
func countriesFromRadioBrowser(sources []radiobrowser.Country) []Country {
	countries := make([]Country, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		country := countryFromRadioBrowser(source)
		if country.Name == "" {
			continue
		}
		if _, exists := seen[country.Code]; exists {
			continue
		}
		seen[country.Code] = struct{}{}
		countries = append(countries, country)
	}
	return countries
}

// stationCountryName returns a CLDR English name based on the station's ISO code.
func stationCountryName(station radio.Station) string {
	return countryNameForCode(station.CountryCode)
}

// countryNameForCode converts an ISO region code into its CLDR English name.
func countryNameForCode(code string) string {
	region, err := language.ParseRegion(strings.ToUpper(strings.TrimSpace(code)))
	if err != nil {
		return ""
	}
	return display.English.Regions().Name(region)
}

// countryCodesByName maps Radio Browser names and their CLDR names to ISO codes.
func countryCodesByName(countries []radiobrowser.Country) map[string]string {
	codes := make(map[string]string, len(countries)*2)
	for _, source := range countries {
		code := strings.ToUpper(strings.TrimSpace(source.Code))
		if code == "" {
			continue
		}
		codes[countryNameKey(source.Name)] = code
		codes[countryNameKey(countryNameForCode(code))] = code
	}
	return codes
}

// withCountryCode fills a legacy station's missing code from its saved country name.
func withCountryCode(station radio.Station, codes map[string]string) (radio.Station, bool) {
	if station.CountryCode != "" || strings.TrimSpace(station.Country) == "" {
		return station, false
	}
	code := codes[countryNameKey(station.Country)]
	if code == "" {
		return station, false
	}
	station.CountryCode = code
	return station, true
}

// countryNameKey normalizes country names for matching saved and directory values.
func countryNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
