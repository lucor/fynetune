package hls

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asticode/go-astits"
	"github.com/bluenviron/gohlslib/v2"
	"go.lucor.dev/fynetune/internal/metadata"
)

func TestOpenTimedID3UsesExistingSegmentDownloads(t *testing.T) {
	frames := bytes.Repeat(testADTSFrame([]byte{0x42, 0x43}), 2)
	segments := [][]byte{
		metadataTS(t, 450000, id3Fields(map[string]string{"TPE1": "Artist", "TIT2": "First", "TALB": "Album"}, false), frames),
		metadataTS(t, 454180, id3Fields(map[string]string{"TIT2": "Next"}, false), frames),
	}
	for _, packed := range []bool{false, true} {
		t.Run(map[bool]string{false: "MPEG-TS", true: "packed AAC"}[packed], func(t *testing.T) {
			var requests [2]atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/radio.m3u8" {
					prefix := "#EXTM3U\n#EXT-X-TARGETDURATION:1\n"
					if packed {
						prefix += "#EXT-X-START:TIME-OFFSET=0\n"
					}
					_, _ = io.WriteString(w, prefix+"#EXTINF:1,\nfirst.ts\n#EXTINF:1,\nsecond.ts\n#EXT-X-ENDLIST\n")
					return
				}
				index := 0
				if r.URL.Path == "/second.ts" {
					index = 1
				}
				requests[index].Add(1)
				if packed {
					title := []string{"First", "Next"}[index]
					_, _ = w.Write(append(id3Fields(map[string]string{"TIT2": title}, false), frames...))
				} else {
					_, _ = w.Write(segments[index])
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			events := make(chan metadata.Event, 8)
			stream, _, err := New(server.Client()).Open(ctx, server.URL+"/radio.m3u8", func(event metadata.Event) { events <- event })
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			audio, err := io.ReadAll(stream)
			if (err != nil && !errors.Is(err, gohlslib.ErrClientEOS)) || !bytes.Equal(audio, bytes.Repeat(frames, 2)) {
				t.Fatalf("audio = %x, error = %v", audio, err)
			}
			for _, title := range []string{"First", "Next"} {
				select {
				case event := <-events:
					if event.Kind != metadata.Replace || event.Source != metadata.SourceID3 || event.Track.Title != title {
						t.Fatalf("event = %+v, want title %q", event, title)
					}
				case <-ctx.Done():
					t.Fatal("missing metadata event")
				}
			}
			for i := range requests {
				if count := requests[i].Load(); count != 1 {
					t.Fatalf("segment %d downloaded %d times", i, count)
				}
			}
		})
	}
}

func TestTimedMetadataWaitsForAudioAndResetsAtDiscontinuity(t *testing.T) {
	frames := bytes.Repeat(testADTSFrame([]byte{0x42, 0x43}), 2)
	var events []metadata.Event
	m := newHLSMetadata(context.Background(), func(event metadata.Event) { events = append(events, event) })
	defer m.close()
	m.observe(metadataTS(t, 450000, id3Fields(map[string]string{"TPE1": "Artist", "TIT2": "First", "TALB": "Album"}, false), frames), false)
	m.observe(metadataTS(t, 454180, id3Fields(map[string]string{"TIT2": "Next"}, true), frames), false)
	m.configure(false)
	if len(events) != 0 {
		t.Fatal("download completion emitted metadata before audio consumption")
	}
	m.advance(0)
	if len(events) != 1 || events[0].Track.Title != "First" {
		t.Fatalf("first audio metadata = %+v", events)
	}
	m.advance(4179)
	if len(events) != 1 {
		t.Fatal("future track emitted early")
	}
	m.advance(4180)
	if len(events) != 2 || events[1].Track.Artist != "Artist" || events[1].Track.Album != "Album" || events[1].Track.Title != "Next" {
		t.Fatalf("standard partial update = %+v", events)
	}
	// A repeated tag must not create an extra event.
	m.observe(metadataTS(t, 458360, id3Fields(map[string]string{"TIT2": "Next"}, true), frames), false)
	m.advance(8360)
	if len(events) != 2 {
		t.Fatalf("duplicate tag emitted metadata: %+v", events)
	}
	m.observe(metadataTS(t, 462540, id3Fields(map[string]string{"TIT2": "New program"}, true), frames), true)
	m.advance(12540)
	if len(events) != 4 || events[2].Kind != metadata.Clear || events[3].Track.Artist != "" || events[3].Track.Album != "" || events[3].Track.Title != "New program" {
		t.Fatalf("discontinuity retained stale fields: %+v", events)
	}
	m.close()
	m.observe(metadataTS(t, 466720, id3Fields(map[string]string{"TIT2": "Late"}, false), frames), false)
	m.advance(20000)
	if len(events) != 4 {
		t.Fatal("closed attempt emitted metadata")
	}
}

func TestTimedMetadataRolloverAndMissingOrMalformedTags(t *testing.T) {
	frames := bytes.Repeat(testADTSFrame([]byte{0x42, 0x43}), 2)
	origin := (int64(1) << 33) - 4180
	var events []metadata.Event
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newHLSMetadata(ctx, func(event metadata.Event) { events = append(events, event) })
	defer m.close()
	m.observe(metadataTS(t, origin, id3Fields(map[string]string{"TIT2": "First"}, false), frames), false)
	m.configure(false)
	m.advance(0)
	m.observe(metadataTS(t, 0, []byte("ID3\x04\x00\x00\x7f\x7f\x7f\x7f"), frames), false)
	m.advance(4180)
	if len(events) != 1 {
		t.Fatal("malformed tag changed the track")
	}
	m.observe(metadataTS(t, 4180, id3Fields(map[string]string{"TIT2": "Next"}, false), frames), false)
	m.advance(8359)
	if len(events) != 1 {
		t.Fatal("rollover caused early metadata")
	}
	m.advance(8360)
	if len(events) != 2 || events[1].Track.Title != "Next" {
		t.Fatalf("rollover metadata = %+v", events)
	}
	cancel()
	m.observe(metadataTS(t, 8360, id3Fields(map[string]string{"TIT2": "Cancelled"}, false), frames), false)
	m.advance(20000)
	if len(events) != 2 {
		t.Fatal("cancelled attempt emitted metadata")
	}
}

func TestMetadataHTTPBodyPreservesInputAndRequiresCompleteDownload(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{"ordinary response", []byte("unchanged audio"), true},
		{"oversized response", bytes.Repeat([]byte{0x42}, maxPackedSegmentBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			body := &metadataBody{ReadCloser: io.NopCloser(bytes.NewReader(tc.data)), complete: func(data []byte) {
				called = true
				if !bytes.Equal(data, tc.data) {
					t.Error("observer received different bytes")
				}
			}}
			data, err := io.ReadAll(body)
			if err != nil || !bytes.Equal(data, tc.data) || called != tc.want {
				t.Fatalf("response changed: error %v, callback %t", err, called)
			}
		})
	}
	called := false
	body := &metadataBody{ReadCloser: io.NopCloser(bytes.NewReader([]byte("partial"))), complete: func([]byte) { called = true }}
	_, _ = body.Read(make([]byte, 1))
	_ = body.Close()
	if called {
		t.Fatal("partial download emitted metadata")
	}
}

func TestMetadataPlaylistDiscontinuityAndByteRanges(t *testing.T) {
	frames := bytes.Repeat(testADTSFrame([]byte{0x42, 0x43}), 2)
	var events []metadata.Event
	m := newHLSMetadata(context.Background(), func(event metadata.Event) { events = append(events, event) })
	defer m.close()
	tap := &metadataTransport{metadata: m, segments: make(map[string]metadataSegmentRef), epochs: make(map[string]int)}
	tap.observe("https://example.invalid/radio.m3u8", "", []byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\n#EXT-X-BYTERANGE:10@0\nstream.ts\n#EXT-X-DISCONTINUITY\n#EXTINF:1,\n#EXT-X-BYTERANGE:10@10\nstream.ts\n"))
	tap.observe("https://example.invalid/stream.ts", "bytes=0-9", metadataTS(t, 450000, id3Fields(map[string]string{"TIT2": "First"}, false), frames))
	m.configure(false)
	m.advance(0)
	tap.observe("https://example.invalid/stream.ts", "bytes=10-19", metadataTS(t, 454180, id3Fields(map[string]string{"TIT2": "Next"}, false), frames))
	m.advance(4180)
	if len(events) != 3 || events[1].Kind != metadata.Clear || events[2].Track.Title != "Next" {
		t.Fatalf("playlist discontinuity = %+v", events)
	}
}

// metadataTS creates a complete audio segment with a declared timed-ID3 PID.
func metadataTS(t *testing.T, pts int64, tag, audio []byte) []byte {
	t.Helper()
	var data bytes.Buffer
	mux := astits.NewMuxer(context.Background(), &data)
	for _, stream := range []astits.PMTElementaryStream{
		{ElementaryPID: 256, StreamType: astits.StreamTypeADTS},
		{ElementaryPID: 257, StreamType: astits.StreamTypeMetadata},
	} {
		if err := mux.AddElementaryStream(stream); err != nil {
			t.Fatal(err)
		}
	}
	mux.SetPCRPID(256)
	for _, sample := range []struct {
		pid    uint16
		stream uint8
		data   []byte
	}{{257, 0xbd, tag}, {256, 0xc0, audio}} {
		if _, err := mux.WriteData(&astits.MuxerData{PID: sample.pid, PES: &astits.PESData{
			Data: sample.data, Header: &astits.PESHeader{StreamID: sample.stream, OptionalHeader: &astits.PESOptionalHeader{
				MarkerBits: 2, DataAlignmentIndicator: true, PTSDTSIndicator: astits.PTSDTSIndicatorOnlyPTS,
				PTS: &astits.ClockReference{Base: pts},
			}},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	return data.Bytes()
}

func id3Fields(fields map[string]string, update bool) []byte {
	var body []byte
	flags := byte(0)
	if update {
		flags, body = 0x40, []byte{0, 0, 0, 7, 1, 0x40, 0}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		payload := append([]byte{3}, []byte(fields[key])...)
		body = append(body, []byte(key)...)
		body = append(body, hlsSyncsafe(len(payload))...)
		body = append(body, 0, 0)
		body = append(body, payload...)
	}
	header := []byte{'I', 'D', '3', 4, 0, flags}
	header = append(header, hlsSyncsafe(len(body))...)
	return append(header, body...)
}

func hlsSyncsafe(n int) []byte {
	return []byte{byte(n >> 21 & 127), byte(n >> 14 & 127), byte(n >> 7 & 127), byte(n & 127)}
}
