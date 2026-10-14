package hls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/asticode/go-astits"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/codec/aac"
	"go.lucor.dev/fynetune/internal/retry"
)

// TestOpenReassemblesAACAcrossPESAndSegments reproduces radio transport streams
// whose PES payloads end inside an ADTS frame, including at a segment boundary.
func TestOpenReassemblesAACAcrossPESAndSegments(t *testing.T) {
	frame := testADTSFrame(bytes.Repeat([]byte{0x42}, 500))
	wire := bytes.Repeat(frame, 6)
	boundary := len(frame)*3 + 200
	segments := [][]byte{
		muxAACPayloads(t, wire[:700], wire[700:boundary]),
		muxAACPayloads(t, wire[boundary:boundary+100], wire[boundary+100:]),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/radio.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nfirst.ts\n#EXTINF:2,\nsecond.ts\n#EXT-X-ENDLIST\n")
		case "/first.ts":
			_, _ = w.Write(segments[0])
		case "/second.ts":
			_, _ = w.Write(segments[1])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, _, err := New(server.Client()).Open(ctx, server.URL+"/radio.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	frames := aac.NewFramer(stream)
	for i := 0; i < 6; i++ {
		got, err := frames.ReadFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(got.AccessUnit, frame[7:]) {
			t.Fatalf("frame %d payload differs", i)
		}
	}
	if _, err := frames.ReadFrame(); err != io.EOF {
		t.Fatalf("end of stream = %v, want EOF", err)
	}
}

func TestShouldRetryAACSegments(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "DTS startup failure", err: fmt.Errorf("difference between DTS and system time is too big"), want: true},
		{name: "ADTS length failure", err: fmt.Errorf("unable to decode ADTS: invalid frame length"), want: true},
		{name: "unrelated HLS failure", err: fmt.Errorf("HLS playlist request failed"), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRetryAACSegments(tc.err); got != tc.want {
				t.Fatalf("shouldRetryAACSegments(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestDTSFailureDuringDecoderProbeRemainsRetryable reproduces a timestamp jump
// after the first audio has made Open succeed, while a decoder is still probing.
func TestDTSFailureDuringDecoderProbeRemainsRetryable(t *testing.T) {
	wire := bytes.Repeat(testADTSFrame([]byte{0x42, 0x43}), 2)
	segment := muxAACPayloadsWithPTSInterval(t, 30*90000, wire, wire)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/radio.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:40\n#EXTINF:40,\nsegment.ts\n#EXT-X-ENDLIST\n")
			return
		}
		_, _ = w.Write(segment)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, info, err := New(server.Client()).Open(ctx, server.URL+"/radio.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	registry := codec.NewRegistry()
	if err := registry.Register(codec.NameAAC, func(reader io.Reader) (codec.Decoder, error) {
		got, readErr := io.ReadAll(reader)
		if !bytes.Equal(got, wire) {
			t.Errorf("audio before interruption = % x, want % x", got, wire)
		}
		return nil, readErr
	}); err != nil {
		t.Fatal(err)
	}
	_, err = registry.Decode(stream, info)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !retry.ShouldRetry(err) {
		t.Fatalf("decoder probe error = %v, want retryable stream interruption", err)
	}
}

// TestAACFallbackRejectsTrackWithoutADTS prevents a declared AAC track with
// malformed payloads from entering an unbounded live decoder probe.
func TestAACFallbackRejectsTrackWithoutADTS(t *testing.T) {
	segment := muxAACPayloads(t, bytes.Repeat([]byte{0x42}, 1000))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/radio.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nsegment.ts\n#EXT-X-ENDLIST\n")
			return
		}
		_, _ = w.Write(segment)
	}))
	defer server.Close()
	stream, _, err := New(server.Client()).openAACSegments(context.Background(), server.URL+"/radio.m3u8")
	if stream != nil {
		_ = stream.Close()
	}
	if !errors.Is(err, errNotPackedAAC) {
		t.Fatalf("fallback error = %v, want rejected AAC data", err)
	}
}

// muxAACPayloads creates complete PES packets with arbitrary AAC byte boundaries.
func muxAACPayloads(t *testing.T, payloads ...[]byte) []byte {
	return muxAACPayloadsWithPTSInterval(t, 90000, payloads...)
}

func muxAACPayloadsWithPTSInterval(t *testing.T, interval int64, payloads ...[]byte) []byte {
	t.Helper()
	var output bytes.Buffer
	muxer := astits.NewMuxer(context.Background(), &output)
	const pid = 256
	if err := muxer.AddElementaryStream(astits.PMTElementaryStream{ElementaryPID: pid, StreamType: astits.StreamTypeADTS}); err != nil {
		t.Fatal(err)
	}
	muxer.SetPCRPID(pid)
	for i, payload := range payloads {
		_, err := muxer.WriteData(&astits.MuxerData{
			PID: pid,
			PES: &astits.PESData{Data: payload, Header: &astits.PESHeader{
				StreamID: 0xc0,
				OptionalHeader: &astits.PESOptionalHeader{
					MarkerBits: 2, PTSDTSIndicator: astits.PTSDTSIndicatorOnlyPTS,
					PTS: &astits.ClockReference{Base: int64(i) * interval},
				},
			}},
		})
		if err != nil {
			t.Fatal(fmt.Errorf("mux PES: %w", err))
		}
	}
	return output.Bytes()
}
