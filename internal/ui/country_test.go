package ui

import (
	"testing"

	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

func TestStationCountryNameUsesCLDRNameFromCode(t *testing.T) {
	station := radio.Station{Country: "The United States of America", CountryCode: "US"}
	if got, want := stationCountryName(station), "United States"; got != want {
		t.Fatalf("stationCountryName() = %q, want %q", got, want)
	}
}

func TestCountriesFromRadioBrowserDeduplicatesNormalizedCodes(t *testing.T) {
	sources := []radiobrowser.Country{
		{Name: "Italy", Code: "IT"},
		{Name: "Italy", Code: "it"},
		{Name: "United States", Code: "US"},
	}

	countries := countriesFromRadioBrowser(sources)
	if len(countries) != 2 {
		t.Fatalf("countries = %#v, want two unique ISO codes", countries)
	}
	if countries[0].Code != "IT" || countries[0].Name != "Italy" || countries[1].Code != "US" {
		t.Fatalf("countries = %#v, want Italy and United States once each", countries)
	}
}

func TestStationCountryNameDoesNotUseRawDirectoryName(t *testing.T) {
	station := radio.Station{Country: "The United States of America"}
	if got := stationCountryName(station); got != "" {
		t.Fatalf("stationCountryName() = %q, want empty without an ISO code", got)
	}
}

func TestWithCountryCodeBackfillsLegacyStation(t *testing.T) {
	station := radio.Station{Country: "The United States of America"}
	codes := countryCodesByName([]radiobrowser.Country{{Name: station.Country, Code: "US"}})

	got, changed := withCountryCode(station, codes)
	if !changed || got.CountryCode != "US" {
		t.Fatalf("withCountryCode() = %#v, changed %t; want country code US", got, changed)
	}
	if got := stationCountryName(got); got != "United States" {
		t.Fatalf("stationCountryName() = %q, want United States", got)
	}
}
