package codec

import (
	"bytes"
	"io"
	"testing"
)

// TestNameFromMIMEType checks known types, MP3 fallbacks, and unknown type handling.
func TestNameFromMIMEType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        CodecName
	}{
		{name: "missing type defaults to MP3", want: NameMP3},
		{name: "malformed type defaults to MP3", contentType: "not a mime type", want: NameMP3},
		{name: "MP3 with parameters", contentType: "Audio/MPEG; charset=binary", want: NameMP3},
		{name: "AAC", contentType: "audio/aacp", want: NameAAC},
		{name: "unknown audio subtype", contentType: "audio/x-custom", want: "X-CUSTOM"},
		{name: "generic octet stream", contentType: "application/octet-stream", want: NameMP3},
		{name: "unknown non-audio type", contentType: "video/x-custom", want: "VIDEO/X-CUSTOM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NameFromMIMEType(tt.contentType); got != tt.want {
				t.Errorf("NameFromMIMEType(%q) = %q, want %q", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestIsADTSHeader(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix []byte
		want   bool
	}{
		{name: "MPEG-4 AAC", prefix: []byte{0xff, 0xf1}, want: true},
		{name: "MPEG-2 AAC", prefix: []byte{0xff, 0xf9}, want: true},
		{name: "AAC with CRC", prefix: []byte{0xff, 0xf0}, want: true},
		{name: "MP3", prefix: []byte{0xff, 0xfb}},
		{name: "invalid sync word", prefix: []byte{0xfe, 0xf1}},
		{name: "short prefix", prefix: []byte{0xff}},
		{name: "empty prefix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isADTSHeader(tc.prefix); got != tc.want {
				t.Fatalf("isADTSHeader(% x) = %t, want %t", tc.prefix, got, tc.want)
			}
		})
	}
}

func TestIdentifyCodec(t *testing.T) {
	oggOpus := make([]byte, 36)
	copy(oggOpus, "OggS")
	oggOpus[26] = 1
	oggOpus[27] = 8
	copy(oggOpus[28:], "OpusHead")

	for _, tc := range []struct {
		name     string
		stream   []byte
		detected CodecName
	}{
		{name: "AAC", stream: []byte{0xff, 0xf1, 0, 0}, detected: NameAAC},
		{name: "Opus in Ogg", stream: oggOpus, detected: NameOPUS},
		{name: "MP3", stream: []byte{0xff, 0xfb, 1}, detected: NameMP3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := StreamInfo{Codec: NameMP3}
			reader, err := identifyCodec(bytes.NewReader(tc.stream), &info)
			if err != nil {
				t.Fatal(err)
			}
			if info.Codec != tc.detected {
				t.Fatalf("identified codec = %q, want %q", info.Codec, tc.detected)
			}
			remaining, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(remaining, tc.stream) {
				t.Fatal("codec probe consumed stream bytes")
			}
		})
	}
}
