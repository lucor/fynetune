package ui

import (
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/radio"
)

func TestRestoreSelectedStation(t *testing.T) {
	favorite := radio.Station{ID: "favorite", Name: "Favorite"}
	discovered := radio.Station{ID: "discovered", Name: "Discovered station"}
	recent := []recentStation{{Station: discovered, PlayedAt: time.Unix(1, 0)}}

	tests := []struct {
		name  string
		id    string
		want  radio.Station
		want0 bool
	}{
		{name: "favorite station", id: favorite.ID, want: favorite},
		{name: "station from recent history", id: discovered.ID, want: discovered},
		{name: "unknown station", id: "missing", want0: true},
		{name: "empty selection", want0: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := restoreSelectedStation(test.id, []radio.Station{favorite}, recent)
			if test.want0 {
				if got.ID != "" {
					t.Fatalf("station = %+v, want empty", got)
				}
				return
			}
			if got.ID != test.want.ID || got.Name != test.want.Name || got.URL != test.want.URL {
				t.Fatalf("station = %+v, want %+v", got, test.want)
			}
		})
	}
}
