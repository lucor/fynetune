package httpstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestOpenRejectsUnsupportedFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	}))
	defer server.Close()
	_, _, err := New(server.Client()).Open(context.Background(), server.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported stream format") {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenRejectsPlaylistsByContentTypeAndURL(t *testing.T) {
	for _, tc := range []struct {
		name, path, contentType, body string
	}{
		{name: "M3U MIME", path: "/stream", contentType: "audio/x-mpegurl"},
		{name: "PLS MIME", path: "/stream", contentType: "audio/x-scpls"},
		{name: "M3U8 URL", path: "/stations/live.m3u8", contentType: "application/octet-stream"},
		{name: "redirected XSPF URL", path: "/redirect", contentType: "application/octet-stream"},
		{name: "M3U body", path: "/stream", contentType: "application/octet-stream", body: "#EXTM3U\nhttp://radio.invalid/live"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/redirect" {
					http.Redirect(w, r, "/stations/live.xspf", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", tc.contentType)
				body := tc.body
				if body == "" {
					w.Header().Set("icy-metaint", "4")
					body = "test"
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			_, _, err := New(server.Client()).Open(context.Background(), server.URL+tc.path, nil)
			if !errors.Is(err, ErrPlaylistUnsupported) || !errors.Is(err, ErrUnsupportedStreamFormat) || !retry.IsPermanent(err) {
				t.Fatalf("Open error = %v; expected playlist and unsupported-format errors", err)
			}
		})
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
