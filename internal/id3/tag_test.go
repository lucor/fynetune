package id3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
)

// These fixtures are synthetic standard tags, not captured broadcasts. The
// JSON in TEXT is deliberately opaque to the parser and has no provider schema.
func TestParseLocalFixtures(t *testing.T) {
	for _, tc := range []struct {
		file string
		text map[string][]string
		user map[string][]string
	}{
		{
			file: "standard-v23.id3",
			text: map[string][]string{
				"TPE1": {"Beyoncé"}, "TIT2": {"Déjà Vu"}, "TALB": {"B'Day"},
				"TYER": {"2006"}, "TIT1": {"Music"}, "TEXT": {"Text writer"},
			},
			user: map[string][]string{"grouping": {"Alternate grouping"}},
		},
		{
			file: "standard-v24.id3",
			text: map[string][]string{
				"TPE1": {"Artist", "Guest"}, "TIT2": {"Song 🎵"}, "TALB": {"Album"},
				"TDRC": {"2026-10-17"}, "TIT1": {"Artist - Song"},
				"TEXT": {`{"type":"music","artist":"Artist","title":"Song"}`},
			},
			user: map[string][]string{"grouping": {"Artist - Song"}},
		},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data := readFixture(t, tc.file)
			original := bytes.Clone(data)
			wire := append(bytes.Clone(data), []byte("audio following the tag")...)
			tag, err := Parse(wire)
			if err != nil {
				t.Fatal(err)
			}
			if tag.Size != len(data) || tag.IsUpdate || !reflect.DeepEqual(tag.TextFrames, tc.text) || !reflect.DeepEqual(tag.UserTextFrames, tc.user) {
				t.Fatalf("Parse() = %+v", tag)
			}
			if !bytes.Equal(wire[:len(data)], original) {
				t.Fatal("Parse modified its input")
			}
		})
	}
}

func TestParseUnsynchronisationAndFramePrefixes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version byte
		flags   byte
		body    []byte
		want    []string
	}{
		{
			name: "v2.3 tag unsynchronisation", version: 3, flags: 0x80,
			body: unsynchronise(testFrame(3, "TIT2", 0, []byte{0, 'A', 0xff, 0xe0})),
			want: []string{"Aÿà"},
		},
		{
			name: "v2.4 grouping and length indicator", version: 4,
			body: testFrame(4, "TIT2", 0x41, append([]byte{7, 0, 0, 0, 5}, []byte{3, 'S', 'o', 'n', 'g'}...)),
			want: []string{"Song"},
		},
		{
			name: "v2.3 grouping identity", version: 3,
			body: testFrame(3, "TIT2", 0x20, []byte{7, 0, 'S', 'o', 'n', 'g'}),
			want: []string{"Song"},
		},
		{
			name: "v2.4 frame unsynchronisation", version: 4,
			body: testFrame(4, "TIT2", 0x02, unsynchronise([]byte{0, 'A', 0xff, 0xe0})),
			want: []string{"Aÿà"},
		},
		{
			name: "v2.4 tag and frame flags undo unsynchronisation once", version: 4, flags: 0x80,
			body: testFrame(4, "TIT2", 0x02, unsynchronise([]byte{0, 'A', 0xff, 0, 'B'})),
			want: []string{"Aÿ", "B"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := testTag(tc.version, tc.flags, tc.body)
			original := bytes.Clone(data)
			tag, err := Parse(data)
			if err != nil || !reflect.DeepEqual(tag.TextFrames["TIT2"], tc.want) {
				t.Fatalf("Parse() = %+v, %v", tag, err)
			}
			if !bytes.Equal(data, original) {
				t.Fatal("Parse modified its input")
			}
		})
	}
}

func TestParseSkipsUnsupportedAndInvalidTextFrames(t *testing.T) {
	for _, version := range []byte{3, 4} {
		compressed, encrypted := byte(0x80), byte(0x40)
		if version == 4 {
			compressed, encrypted = 0x08, 0x04
		}
		body := bytes.Join([][]byte{
			testFrame(version, "TPE1", compressed, []byte{0, 'x'}),
			testFrame(version, "TALB", encrypted, []byte{0, 'x'}),
			testFrame(version, "TEXT", 0, []byte{9, 'x'}),           // Unknown encoding.
			testFrame(version, "TIT1", 0, []byte{3, 0xff}),          // Invalid UTF-8.
			testFrame(version, "TPE2", 0, []byte{1, 0xff, 0xfe, 0}), // Truncated UTF-16.
			testFrame(version, "TCOM", 0, []byte{2, 0xd8, 0}),       // Unpaired surrogate.
			testFrame(version, "TPE3", 0, []byte{1, 0, 'A'}),        // UTF-16 without BOM.
			testFrame(version, "TIT2", 0, []byte{0, 'S', 'o', 'n', 'g'}),
		}, nil)
		tag, err := Parse(testTag(version, 0, body))
		want := map[string][]string{"TIT2": {"Song"}}
		if err != nil || !reflect.DeepEqual(tag.TextFrames, want) {
			t.Fatalf("version %d: Parse() = %+v, %v", version, tag, err)
		}
	}
}

func TestParseExtendedUpdateFlag(t *testing.T) {
	extended := []byte{0, 0, 0, 7, 1, 0x40, 0}
	body := append(extended, testFrame(4, "TIT2", 0, []byte{3, 'S', 'o', 'n', 'g'})...)
	tag, err := Parse(testTag(4, 0x40, body))
	if err != nil || !tag.IsUpdate || !reflect.DeepEqual(tag.TextFrames["TIT2"], []string{"Song"}) {
		t.Fatalf("Parse() = %+v, %v", tag, err)
	}
}

func TestParseUTF16ByteOrdersAndSurrogatePairs(t *testing.T) {
	for _, payload := range [][]byte{
		{1, 0xff, 0xfe, 0x3d, 0xd8, 0, 0xde}, // UTF-16LE with BOM.
		{1, 0xfe, 0xff, 0xd8, 0x3d, 0xde, 0}, // UTF-16BE with BOM.
		{2, 0xd8, 0x3d, 0xde, 0},             // UTF-16BE without BOM.
	} {
		tag, err := Parse(testTag(4, 0, testFrame(4, "TIT2", 0, payload)))
		if err != nil || !reflect.DeepEqual(tag.TextFrames["TIT2"], []string{"😀"}) {
			t.Fatalf("payload %x: Parse() = %+v, %v", payload, tag, err)
		}
	}
}

func TestParseRejectsMismatchedFooter(t *testing.T) {
	data := readFixture(t, "standard-v24.id3")
	data[len(data)-1] ^= 1
	if tag, err := Parse(data); !errors.Is(err, ErrMalformed) || tag.Size != 0 {
		t.Fatalf("Parse() = %+v, %v", tag, err)
	}
}

func TestParseIgnoresPrivateTransportTimestamps(t *testing.T) {
	body := testFrame(4, "PRIV", 0, append([]byte("com.apple.streaming.transportStreamTimestamp\x00"), make([]byte, 8)...))
	tag, err := Parse(testTag(4, 0, body))
	if err != nil || len(tag.TextFrames) != 0 || len(tag.UserTextFrames) != 0 {
		t.Fatalf("private timestamp produced text metadata: %+v, %v", tag, err)
	}
}

func TestParseTruncatedFixtures(t *testing.T) {
	for _, file := range []string{"standard-v23.id3", "standard-v24.id3"} {
		data := readFixture(t, file)
		for n := 3; n < len(data); n++ {
			tag, err := Parse(data[:n])
			if !errors.Is(err, io.ErrUnexpectedEOF) || tag.Size != 0 || len(tag.TextFrames) != 0 || len(tag.UserTextFrames) != 0 {
				t.Fatalf("%s truncated at %d: Parse() = %+v, %v", file, n, tag, err)
			}
		}
	}
}

func TestParseRejectsMalformedTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		err  error
	}{
		{name: "absent", err: ErrNoTag},
		{name: "not ID3", data: []byte("audio bytes"), err: ErrNoTag},
		{name: "unsupported v2.2", data: testTag(2, 0, nil), err: ErrUnsupportedVersion},
		{name: "reserved header flags", data: testTag(4, 1, nil), err: ErrMalformed},
		{name: "invalid tag size", data: []byte{'I', 'D', '3', 4, 0, 0, 0x80, 0, 0, 0}, err: ErrMalformed},
		{name: "excessive tag size", data: []byte{'I', 'D', '3', 4, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f}, err: ErrTooLarge},
		{name: "truncated frame header", data: testTag(4, 0, []byte("TIT2")), err: ErrMalformed},
		{name: "invalid frame ID", data: testTag(4, 0, testFrame(4, "TiT2", 0, []byte{3, 'A'})), err: ErrMalformed},
		{name: "invalid frame size", data: testTag(4, 0, []byte{'T', 'I', 'T', '2', 0x80, 0, 0, 0, 0, 0}), err: ErrMalformed},
		{name: "frame overflows tag", data: testTag(3, 0, []byte{'T', 'I', 'T', '2', 0xff, 0xff, 0xff, 0xff, 0, 0}), err: ErrMalformed},
		{name: "malformed padding", data: testTag(4, 0, []byte{0, 1}), err: ErrMalformed},
		{name: "missing extended header", data: testTag(4, 0x40, nil), err: ErrMalformed},
		{name: "extended header overflows tag", data: testTag(3, 0x40, []byte{0xff, 0xff, 0xff, 0xff}), err: ErrMalformed},
		{name: "invalid extended header size", data: testTag(4, 0x40, []byte{0, 0, 0, 3, 1, 0}), err: ErrMalformed},
		{name: "extended flag missing data length", data: testTag(4, 0x40, []byte{0, 0, 0, 6, 1, 0x40}), err: ErrMalformed},
		{name: "missing footer", data: testTag(4, 0x10, make([]byte, 10)), err: io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tag, err := Parse(tc.data)
			if !errors.Is(err, tc.err) || tag.Size != 0 || len(tag.TextFrames) != 0 || len(tag.UserTextFrames) != 0 {
				t.Fatalf("Parse() = %+v, %v; want %v", tag, err, tc.err)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	for _, file := range []string{"standard-v23.id3", "standard-v24.id3"} {
		data, err := os.ReadFile("testdata/" + file)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte("ID3"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tag, err := Parse(data)
		if err == nil && (tag.Size < 10 || tag.Size > len(data) || tag.Size > MaxTagSize) {
			t.Fatalf("invalid tag size %d for %d input bytes", tag.Size, len(data))
		}
		if err != nil && (tag.Size != 0 || len(tag.TextFrames) != 0 || len(tag.UserTextFrames) != 0) {
			t.Fatalf("partial tag returned on error: %+v, %v", tag, err)
		}
	})
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func testTag(version, flags byte, body []byte) []byte {
	header := []byte{'I', 'D', '3', version, 0, flags}
	header = append(header, testSyncsafe(len(body))...)
	return append(header, body...)
}

func testFrame(version byte, id string, flags byte, payload []byte) []byte {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)))
	if version == 4 {
		copy(size[:], testSyncsafe(len(payload)))
	}
	frame := append([]byte(id), size[:]...)
	frame = append(frame, 0, flags)
	return append(frame, payload...)
}

func testSyncsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7f), byte(n >> 14 & 0x7f), byte(n >> 7 & 0x7f), byte(n & 0x7f)}
}

func unsynchronise(data []byte) []byte {
	var result []byte
	for i, b := range data {
		result = append(result, b)
		if b == 0xff && (i+1 == len(data) || data[i+1] == 0 || data[i+1] >= 0xe0) {
			result = append(result, 0)
		}
	}
	return result
}
