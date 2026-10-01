package httpstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/playlist"
	"go.lucor.dev/fynetune/internal/retry"
	"go.lucor.dev/fynetune/internal/version"
)

func TestOpenNegotiatesAndFiltersICY(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Icy-MetaData") != "1" {
			t.Error("request did not ask for ICY metadata")
		}
		if got, want := r.Header.Get("User-Agent"), version.UserAgent(); got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("icy-metaint", "4")
		_, _ = io.WriteString(w, "abcd\x02")
		metadata := make([]byte, 32)
		copy(metadata, "StreamTitle='Artist - Song';")
		_, _ = w.Write(metadata)
		_, _ = io.WriteString(w, "efgh")
	}))
	defer server.Close()

	var title string
	reader, info, err := New(server.Client()).Open(context.Background(), server.URL, func(metadata string) { title = metadata })
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdefgh" || title != "Artist - Song" {
		t.Fatalf("audio=%q metadata=%q", data, title)
	}
	if info.Codec != "MP3" || info.MIMEType != "audio/mpeg" {
		t.Fatalf("stream info = %+v", info)
	}
}

func TestOpenRoutesHLSManifestToHLSClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n")
	}))
	defer server.Close()
	_, _, err := New(server.Client()).Open(context.Background(), server.URL, nil)
	if err == nil || errors.Is(err, playlist.ErrUnsupportedHLS) {
		t.Fatalf("HLS client error = %v, want a client playback error", err)
	}
}

func TestOpenResolvesPlaylists(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		start   string
		want    string
	}{
		{
			name: "direct stream unchanged",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = io.WriteString(w, "direct audio bytes")
			},
			start: "/live", want: "/live",
		},
		{
			name: "HTTP redirect",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/radio/list.m3u", http.StatusFound)
					return
				}
				if r.URL.Path == "/radio/list.m3u" {
					w.Header().Set("Content-Type", "audio/x-mpegurl")
					_, _ = io.WriteString(w, "#EXTM3U\n../audio.mp3\n")
					return
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = io.WriteString(w, "stream bytes")
			},
			start: "/start", want: "/audio.mp3",
		},
		{
			name: "PLS",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/station" {
					w.Header().Set("Content-Type", "audio/x-scpls")
					_, _ = io.WriteString(w, "[playlist]\nFile1=/stream.mp3\nTitle1=Radio\n")
					return
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = io.WriteString(w, "stream bytes")
			},
			start: "/station", want: "/stream.mp3",
		},
		{
			name: "fallback to next entry",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/station.m3u" {
					w.Header().Set("Content-Type", "audio/x-mpegurl")
					_, _ = io.WriteString(w, "#EXTM3U\n/missing.mp3\n/backup.mp3\n")
					return
				}
				if r.URL.Path == "/missing.mp3" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = io.WriteString(w, "stream bytes")
			},
			start: "/station.m3u", want: "/backup.mp3",
		},
		{
			name: "nested M3U and relative URL with wrong MIME",
			handler: func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/dir/outer.m3u":
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = io.WriteString(w, "#EXTM3U\ninner.m3u\n")
				case "/dir/inner.m3u":
					w.Header().Set("Content-Type", "text/plain")
					_, _ = io.WriteString(w, "#EXTM3U\n../live.mp3\n")
				default:
					w.Header().Set("Content-Type", "audio/mpeg")
					_, _ = io.WriteString(w, "stream bytes")
				}
			},
			start: "/dir/outer.m3u", want: "/live.mp3",
		},
		{
			name: "content signature with incorrect content type",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/list" {
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = io.WriteString(w, "#EXTM3U\n/live.mp3\n")
					return
				}
				w.Header().Set("Content-Type", "audio/mpeg")
				_, _ = io.WriteString(w, "stream bytes")
			},
			start: "/list", want: "/live.mp3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			stream, info, err := New(server.Client()).Open(context.Background(), server.URL+tc.start, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			resolved, err := url.Parse(info.URL)
			if err != nil || resolved.Path != tc.want {
				t.Fatalf("resolved URL = %q, want path %q (err %v)", info.URL, tc.want, err)
			}
		})
	}
}

func TestOpenRejectsOversizedPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/x-mpegurl")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, maxPlaylistBytes+1))
	}))
	defer server.Close()
	_, _, err := New(server.Client()).Open(context.Background(), server.URL+"/list.m3u", nil)
	if !errors.Is(err, ErrPlaylistTooLarge) || !retry.IsPermanent(err) {
		t.Fatalf("Open error = %v", err)
	}
}

func TestOpenPlaylistPreservesTransientEntryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/station.m3u" {
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			_, _ = io.WriteString(w, "#EXTM3U\n/flaky.mp3\n")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, _, err := New(server.Client()).Open(context.Background(), server.URL+"/station.m3u", nil)
	if !errors.Is(err, ErrHTTPStatus) || retry.IsPermanent(err) {
		t.Fatalf("Open error = %v, permanent = %t", err, retry.IsPermanent(err))
	}
}

func TestOpenRejectsPlaylistNestingOverflowAndLoop(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(int) string
	}{
		{name: "depth", path: func(index int) string { return fmt.Sprintf("/list%d.m3u", index) }},
		{name: "loop", path: func(index int) string {
			if index == 0 {
				return "/loop.m3u"
			}
			return "/loop.m3u"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index := 0
				if _, err := fmt.Sscanf(r.URL.Path, "/list%d.m3u", &index); err == nil {
					w.Header().Set("Content-Type", "audio/x-mpegurl")
					_, _ = fmt.Fprintf(w, "#EXTM3U\n%s\n", tc.path(index+1))
					return
				}
				w.Header().Set("Content-Type", "audio/x-mpegurl")
				_, _ = fmt.Fprintf(w, "#EXTM3U\n%s\n", tc.path(index+1))
			}))
			defer server.Close()
			_, _, err := New(server.Client()).Open(context.Background(), server.URL+tc.path(0), nil)
			if err == nil || !retry.IsPermanent(err) {
				t.Fatalf("Open error = %v, want permanent playlist resolution failure", err)
			}
		})
	}
}

func TestOpenM3U8RoutesToHLSClient(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,segment\npart.ts\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	stream, _, err := New(server.Client()).Open(ctx, server.URL+"/radio.m3u8", nil)
	if stream != nil {
		defer stream.Close()
	}
	if errors.Is(err, playlist.ErrUnsupportedHLS) {
		t.Fatalf("HLS manifest was rejected by playlist resolver: %v", err)
	}
	if requests.Load() < 2 {
		t.Fatalf("HLS client did not refetch the manifest; request count = %d", requests.Load())
	}
}

func TestOpenRejectsOtherUnsupportedPlaylistFormats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/station.xspf", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "<?xml version=\"1.0\"?><playlist/>")
	}))
	defer server.Close()
	_, _, err := New(server.Client()).Open(context.Background(), server.URL+"/redirect", nil)
	if !errors.Is(err, ErrUnsupportedStreamFormat) || !errors.Is(err, ErrPlaylistUnsupported) || !retry.IsPermanent(err) {
		t.Fatalf("Open error = %v", err)
	}
}

func TestHTTPStatusRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		status    int
		permanent bool
	}{
		{status: http.StatusNotFound, permanent: true},
		{status: http.StatusUnauthorized, permanent: true},
		{status: http.StatusRequestTimeout, permanent: false},
		{status: http.StatusTooManyRequests, permanent: false},
		{status: http.StatusServiceUnavailable, permanent: false},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			defer server.Close()
			_, _, err := New(server.Client()).Open(context.Background(), server.URL, nil)
			if !errors.Is(err, ErrHTTPStatus) || retry.IsPermanent(err) != tc.permanent {
				t.Fatalf("status %d error = %v, permanent=%v", tc.status, err, retry.IsPermanent(err))
			}
		})
	}
}

func TestOpenCancellationClosesNetworkBody(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	reader, _, err := New(server.Client()).Open(ctx, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not receive stream request")
	}
	cancel()
	_ = reader.Close()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("request context was not canceled")
	}
}

func TestOpenCancellationWhileSniffingUnknownContentType(t *testing.T) {
	started := make(chan struct{})
	serverCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(serverCanceled)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type openResult struct{ err error }
	result := make(chan openResult, 1)
	go func() {
		reader, _, err := New(server.Client()).Open(ctx, server.URL, nil)
		if reader != nil {
			_ = reader.Close()
		}
		result <- openResult{err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("server did not receive playlist probe")
	}
	cancel()
	select {
	case got := <-result:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("Open error = %v, want context cancellation", got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Open did not stop after context cancellation")
	}
	select {
	case <-serverCanceled:
	case <-time.After(time.Second):
		t.Fatal("server request was not canceled")
	}
}
