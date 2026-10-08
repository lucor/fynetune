package ui

import (
	"strings"

	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// Country is the UI representation of a country, with a stable ISO code and display name.
type Country struct {
	Code string
	Name string
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

// countryFromRadioBrowser uses the CLDR English region name when its code is recognized.
func countryFromRadioBrowser(source radiobrowser.Country) Country {
	code := strings.ToUpper(strings.TrimSpace(source.Code))
	name := strings.TrimSpace(source.Name)
	if displayName := countryNameForCode(code); displayName != "" {
		name = displayName
	}
	return Country{Code: code, Name: name}
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
