package playlist

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePLS(t *testing.T) {
	input := "\xef\xbb\xbf[playlist]\r\ntItLe2 = Backup\r\nVersion=2\r\nFILE2=https://backup.example/live\r\nNumberOfEntries=99\r\nTitle1= Main Radio \r\nLength1=-1\r\nfile1 = https://main.example/live\r\nUnknown=ignored\r\n"
	got, err := ParsePLS(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{URL: "https://main.example/live", Title: "Main Radio"}, {URL: "https://backup.example/live", Title: "Backup"}}
	if len(got.Entries) != len(want) {
		t.Fatalf("entries = %#v", got.Entries)
	}
	for i := range want {
		if got.Entries[i] != want[i] {
			t.Fatalf("entry %d = %#v, want %#v", i, got.Entries[i], want[i])
		}
	}
}

func TestParsePLSSingleEntryWithoutHeader(t *testing.T) {
	got, err := ParsePLS(strings.NewReader("File7=https://radio.example/live\n"))
	if err != nil || len(got.Entries) != 1 || got.Entries[0].URL != "https://radio.example/live" {
		t.Fatalf("ParsePLS = %#v, %v", got, err)
	}
}

func TestParsePLSNoURLsAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  error
	}{{"", ErrEmpty}, {"[playlist]\nFile1= \nNumberOfEntries=1\n", ErrNoValidEntries}} {
		_, err := ParsePLS(strings.NewReader(tc.input))
		if !errors.Is(err, tc.want) {
			t.Errorf("input %q error = %v, want %v", tc.input, err, tc.want)
		}
	}
}
