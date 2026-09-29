package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"go.lucor.dev/fynetune/internal/radio"
)

const stationMigrationVersion = 3
const recentStationsPreference = "recent_stations"

type Settings struct {
	AutoPlay, Reconnect, StartMinimized bool
	Stations                            []radio.Station
	Selected                            string
	DiscoveryCountry                    string
	Volume                              float64
}

type RecentStation struct {
	Station  radio.Station `json:"station"`
	PlayedAt time.Time     `json:"played_at"`
}

type Store struct{ p fyne.Preferences }

func New(a fyne.App) *Store { return &Store{p: a.Preferences()} }
func (s *Store) Load() (Settings, error) {
	v := Settings{Reconnect: true, Volume: .75}
	raw := s.p.String("stations")
	if raw != "" {
		var rows []radio.Station
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return v, fmt.Errorf("saved stations are invalid: %w", err)
		}
		v.Stations = rows
		if s.p.Int("station_defaults_version") < stationMigrationVersion {
			v.Stations = removeBundledStations(v.Stations)
			if err := s.SaveStations(v.Stations); err != nil {
				return v, fmt.Errorf("migrate saved stations: %w", err)
			}
		}
	}
	v.Selected = s.p.String("selected_station_id")
	v.DiscoveryCountry = s.p.String("discovery_country")
	v.Volume = s.p.Float("volume")
	if v.Volume == 0 && s.p.String("volume") == "" {
		v.Volume = .75
	}
	v.AutoPlay = s.p.Bool("auto_play")
	v.Reconnect = s.p.BoolWithFallback("reconnect_enabled", true)
	v.StartMinimized = s.p.Bool("start_minimized")
	return v, nil
}

func removeBundledStations(stations []radio.Station) []radio.Station {
	remaining := stations[:0]
	for _, station := range stations {
		if !isUnmodifiedBundledStation(station) {
			remaining = append(remaining, station)
		}
	}
	return remaining
}

func isUnmodifiedBundledStation(station radio.Station) bool {
	switch station.ID {
	case "virgin-radio-italia":
		return station.Name == "Virgin Radio Italia" && station.URL == "https://icecast.unitedradio.it/Virgin.mp3" &&
			(station.Homepage == "" || station.Homepage == "https://www.virginradio.it/") && station.Favicon == "" && station.ResolvedURL == ""
	case "rtl-1025":
		return station.Name == "RTL 102.5" && station.URL == "https://streamingv2.shoutcast.com/rtl-1025" &&
			(station.Homepage == "" || station.Homepage == "https://www.rtl.it/") && station.Favicon == "" && station.ResolvedURL == ""
	default:
		return false
	}
}
func (s *Store) SaveStations(rows []radio.Station) error {
	b, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	s.p.SetString("stations", string(b))
	s.p.SetInt("station_defaults_version", stationMigrationVersion)
	return nil
}

func (s *Store) LoadRecent() ([]RecentStation, error) {
	raw := s.p.String(recentStationsPreference)
	if raw == "" {
		return nil, nil
	}
	var recent []RecentStation
	if err := json.Unmarshal([]byte(raw), &recent); err != nil {
		return nil, fmt.Errorf("saved recent stations are invalid: %w", err)
	}
	return recent, nil
}

func (s *Store) SaveRecent(recent []RecentStation) error {
	data, err := json.Marshal(recent)
	if err != nil {
		return fmt.Errorf("encode recent stations: %w", err)
	}
	s.p.SetString(recentStationsPreference, string(data))
	return nil
}

func (s *Store) SaveSettings(v Settings) error {
	if v.Volume < 0 || v.Volume > 1 {
		return errors.New("volume must be between zero and one")
	}
	s.p.SetString("selected_station_id", v.Selected)
	s.p.SetString("discovery_country", v.DiscoveryCountry)
	s.p.SetFloat("volume", v.Volume)
	s.p.SetBool("auto_play", v.AutoPlay)
	s.p.SetBool("reconnect_enabled", v.Reconnect)
	s.p.SetBool("start_minimized", v.StartMinimized)
	return nil
}
