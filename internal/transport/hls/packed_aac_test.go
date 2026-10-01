package hls

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOpenFallsBackForPackedAACSegments(t *testing.T) {
	wire := append(testADTSFrame([]byte{0x11, 0x22}), testADTSFrame([]byte{0x33, 0x44})...)
	var segmentRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=96000,CODECS=\"mp4a.40.2\"\naudio.m3u8\n")
		case "/audio.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:7\n#EXTINF:2,\nsegment.aac\n#EXT-X-ENDLIST\n")
		case "/segment.aac":
			segmentRequests.Add(1)
			w.Header().Set("Content-Type", "audio/aac")
			_, _ = w.Write(wire)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	stream, info, err := New(server.Client()).Open(context.Background(), server.URL+"/master.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatalf("stream bytes = % x, want % x", got, wire)
	}
	if info.Codec != "AAC" || segmentRequests.Load() < 2 {
		t.Fatalf("stream info = %+v; segment requests = %d", info, segmentRequests.Load())
	}
}

func TestPackedAACFallbackRejectsMPEGTS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/radio.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nsegment.ts\n#EXT-X-ENDLIST\n")
		case "/segment.ts":
			_, _ = w.Write(bytes.Repeat([]byte{0x47}, 188*8))
		}
	}))
	defer server.Close()

	_, _, err := New(server.Client()).Open(context.Background(), server.URL+"/radio.m3u8")
	if err == nil || !errors.Is(err, errNotPackedAAC) {
		t.Fatalf("Open error = %v, want packed AAC fallback rejection", err)
	}
}

func testADTSFrame(payload []byte) []byte {
	length := len(payload) + 7
	return append([]byte{
		0xff, 0xf1,
		0x50,
		0x80 | byte(length>>11)&3,
		byte(length >> 3),
		byte(length&7)<<5 | 0x1f,
		0xfc,
	}, payload...)
}
