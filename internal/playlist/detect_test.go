package playlist

import "testing"

func TestDetect(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, rawURL string
		prefix                    []byte
		want                      Kind
	}{
		{name: "M3U MIME type", contentType: "application/vnd.apple.mpegurl", want: KindM3U},
		{name: "PLS path", rawURL: "https://radio.example/list.pls", want: KindPLS},
		{name: "M3U content", contentType: "text/plain", prefix: []byte("#EXTM3U\n"), want: KindM3U},
		{name: "PLS content", contentType: "text/plain", prefix: []byte("[playlist]\n"), want: KindPLS},
		{name: "unsupported XSPF", contentType: "application/xspf+xml", want: KindUnsupported},
		{name: "audio stream", contentType: "audio/mpeg", want: KindNotPlaylist},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.contentType, tc.rawURL, tc.prefix); got != tc.want {
				t.Fatalf("Detect(%q, %q, %q) = %v, want %v", tc.contentType, tc.rawURL, tc.prefix, got, tc.want)
			}
		})
	}
}
