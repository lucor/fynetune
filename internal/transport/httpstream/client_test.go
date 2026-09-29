package httpstream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
