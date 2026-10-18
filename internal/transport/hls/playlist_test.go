package hls

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
)

func TestAACPlaylistStartOffset(t *testing.T) {
	for _, tc := range []struct {
		attributes string
		wantOffset time.Duration
		wantErr    bool
	}{
		{attributes: "TIME-OFFSET=0"},
		{attributes: "TIME-OFFSET=0.000,PRECISE=YES"},
		{attributes: "TIME-OFFSET=-0.0"},
		{attributes: "TIME-OFFSET=2.5", wantOffset: 2500 * time.Millisecond},
		{attributes: "TIME-OFFSET=-2.5", wantOffset: -2500 * time.Millisecond},
		{attributes: "PRECISE=YES", wantErr: true},
		{attributes: "TIME-OFFSET=invalid", wantErr: true},
	} {
		t.Run(tc.attributes, func(t *testing.T) {
			body := "#EXTM3U\r\n#EXT-X-TARGETDURATION:2\r\n#EXT-X-START:" + tc.attributes + "\r\n#EXTINF:2,\r\nsegment.ts\r\n#EXT-X-ENDLIST\r\n"
			parsed, err := unmarshalAACPlaylist([]byte(body))
			if (err != nil) != tc.wantErr {
				t.Fatalf("parse error = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			media := parsed.(*playlist.Media)
			if tc.wantOffset != 0 && (media.Start == nil || media.Start.TimeOffset != tc.wantOffset) {
				t.Fatalf("start = %+v, want offset %s", media.Start, tc.wantOffset)
			}
			if len(media.Segments) != 1 || media.Segments[0].URI != "segment.ts" {
				t.Fatalf("segments = %+v", media.Segments)
			}
		})
	}
}

// Reproduce France Inter's zero start offset and verify that audio continues
// through master resolution, initial media parsing, and a live playlist reload.
func TestOpenZeroStartOffsetAndReload(t *testing.T) {
	frame := testADTSFrame([]byte{0x11, 0x22})
	segment := muxAACPayloads(t, frame)
	var mediaRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-START:TIME-OFFSET=0\n#EXT-X-STREAM-INF:BANDWIDTH=96000,CODECS=\"mp4a.40.2\"\naudio.m3u8\n")
		case "/audio.m3u8":
			body := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXT-X-MEDIA-SEQUENCE:7\n#EXT-X-START:TIME-OFFSET=0\n#EXTINF:1,\nfirst.ts\n"
			if mediaRequests.Add(1) > 1 {
				body += "#EXTINF:1,\nsecond.ts\n#EXT-X-ENDLIST\n"
			}
			_, _ = io.WriteString(w, body)
		case "/first.ts", "/second.ts":
			_, _ = w.Write(segment)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, _, err := New(server.Client()).Open(ctx, server.URL+"/master.m3u8", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if want := bytes.Repeat(frame, 2); !bytes.Equal(got, want) {
		t.Fatalf("audio = % x, want % x", got, want)
	}
}
