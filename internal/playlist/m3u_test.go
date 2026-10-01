package playlist

import (
	"errors"
	"strings"
	"testing"
)

func TestParseM3U(t *testing.T) {
	got, err := ParseM3U(strings.NewReader("\xef\xbb\xbf#EXTM3U\r\n#EXTINF:-1, Radio One \r\n\r\nhttps://one.example/live\r\n# unknown tag\r\n#EXTVLCOPT:network-caching=1000\r\nhttps://two.example/live\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{URL: "https://one.example/live", Title: "Radio One"}, {URL: "https://two.example/live"}}
	if len(got.Entries) != len(want) {
		t.Fatalf("entries = %#v", got.Entries)
	}
	for i := range want {
		if got.Entries[i] != want[i] {
			t.Fatalf("entry %d = %#v, want %#v", i, got.Entries[i], want[i])
		}
	}
}

func TestParseM3UWithoutHeader(t *testing.T) {
	got, err := ParseM3U(strings.NewReader("  https://radio.example/live  \n"))
	if err != nil || len(got.Entries) != 1 || got.Entries[0].URL != "https://radio.example/live" {
		t.Fatalf("ParseM3U = %#v, %v", got, err)
	}
}

func TestParseM3URejectsHLS(t *testing.T) {
	for _, marker := range []string{"#EXT-X-TARGETDURATION:6", "#EXT-X-STREAM-INF:BANDWIDTH=128000", "#EXT-X-MEDIA-SEQUENCE:1"} {
		_, err := ParseM3U(strings.NewReader("#EXTM3U\n" + marker + "\n"))
		if !errors.Is(err, ErrUnsupportedHLS) {
			t.Errorf("marker %q error = %v", marker, err)
		}
	}
}

func TestParseM3UEmpty(t *testing.T) {
	_, err := ParseM3U(strings.NewReader(" \r\n\t\n"))
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("error = %v", err)
	}
}
