package metadata

import "testing"

func TestTrackString(t *testing.T) {
	for _, tc := range []struct {
		name  string
		track Track
		want  string
	}{
		{
			name:  "structured fields without a raw title",
			track: Track{Artist: "Artist", Title: "Song", Album: "Album", Year: "2000", Parsed: true},
			want:  "Artist - Song - Album - 2000",
		},
		{
			name:  "missing album",
			track: Track{Artist: "Artist", Title: "Song", Year: "2000"},
			want:  "Artist - Song - 2000",
		},
		{
			name:  "partial fields",
			track: Track{Artist: "Artist", Album: "Album"},
			want:  "Artist - Album",
		},
		{
			name:  "unstructured source title",
			track: Track{RawTitle: "Station ID"},
			want:  "Station ID",
		},
		{
			name:  "structured fields take precedence over raw title",
			track: Track{RawTitle: "Original text", Artist: " Artist ", Title: " Song ", Album: "  "},
			want:  "Artist - Song",
		},
		{name: "no metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.track.String(); got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}
