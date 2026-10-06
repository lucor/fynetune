package codec

import "testing"

// TestNameFromMIMEType checks known types, MP3 fallbacks, and unknown type handling.
func TestNameFromMIMEType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        string
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
