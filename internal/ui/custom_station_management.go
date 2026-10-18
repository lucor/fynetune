package ui

import (
	"fmt"
	"log/slog"

	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/storage"
)

// confirmDeleteCustomStation asks before permanently removing entered station data.
func (w *Window) confirmDeleteCustomStation(station radio.Station) {
	d := dialog.NewConfirm(
		"Delete station",
		fmt.Sprintf("Delete %q? This permanently removes the station and its recent history.", station.Name),
		func(confirmed bool) {
			if !confirmed {
				return
			}
			if err := w.deleteCustomStation(station); err != nil {
				dialog.ShowError(err, w.window)
			}
		},
		w.window,
	)
	d.SetConfirmText("Delete")
	d.SetConfirmImportance(widget.HighImportance)
	d.Show()
}

// deleteCustomStation removes a custom station and its persisted references.
func (w *Window) deleteCustomStation(station radio.Station) error {
	if !station.IsCustom() {
		return radio.ErrNotCustomStation
	}

	favorites := make([]radio.Station, 0, len(w.settings.Stations))
	found := false
	for _, existing := range w.settings.Stations {
		if existing.ID == station.ID {
			found = true
			continue
		}
		favorites = append(favorites, existing)
	}
	if !found {
		return radio.ErrNotCustomStation
	}

	recent := make([]storage.RecentStation, 0, len(w.recent))
	for _, entry := range w.recent {
		if entry.Station.ID != station.ID {
			recent = append(recent, entry)
		}
	}

	settings := w.settings
	clearSelection := settings.Selected == station.ID || w.selectedStation.ID == station.ID
	if clearSelection {
		settings.Selected = ""
	}

	if err := w.store.SaveStations(favorites); err != nil {
		return fmt.Errorf("save favorites after deleting station: %w", err)
	}
	if err := w.store.SaveRecent(recent); err != nil {
		if rollbackErr := w.store.SaveStations(w.settings.Stations); rollbackErr != nil {
			slog.Error("could not restore favorites after failed station deletion", "error", rollbackErr)
		}
		return fmt.Errorf("save recent stations after deleting station: %w", err)
	}
	if clearSelection {
		if err := w.store.SaveSettings(settings); err != nil {
			if rollbackErr := w.store.SaveStations(w.settings.Stations); rollbackErr != nil {
				slog.Error("could not restore favorites after failed station deletion", "error", rollbackErr)
			}
			if rollbackErr := w.store.SaveRecent(w.recent); rollbackErr != nil {
				slog.Error("could not restore recent stations after failed station deletion", "error", rollbackErr)
			}
			return fmt.Errorf("clear selected station after deletion: %w", err)
		}
	}

	w.settings.Stations = favorites
	w.settings.Selected = settings.Selected
	w.recent = recent
	if clearSelection {
		if w.radioPlayer != nil && w.radioPlayer.State() != radio.StateStopped {
			w.radioPlayer.Stop()
		}
		w.selectedStation = radio.Station{}
		if w.playerBar != nil {
			w.playerBar.setPlayback(radio.Station{}, "Not playing", false)
		}
		w.updateTray()
	}
	if w.favorites != nil {
		w.favorites.refresh()
	}
	if w.recentSection != nil {
		w.recentSection.refresh()
	}
	if w.home != nil && w.home.rows.list != nil {
		w.home.rows.list.Refresh()
	}
	if w.discover != nil {
		w.discover.rows.refreshFavorites()
	}
	return nil
}
