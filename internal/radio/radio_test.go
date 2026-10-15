package radio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/audio"
	"go.lucor.dev/fynetune/internal/codec"
)

func TestStationJSONAndValidation(t *testing.T) {
	s := Station{ID: "test", Name: "Test station", URL: "https://example.com/radio.mp3", Homepage: "https://example.com"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got Station
	if err = json.Unmarshal(b, &got); err != nil || !reflect.DeepEqual(got, s) {
		t.Fatalf("round trip: %#v, %v", got, err)
	}
	for _, tc := range []struct {
		s       Station
		wantErr bool
	}{{Station{Name: "x", URL: "https://example.com/a.mp3"}, false}, {Station{Name: "", URL: "https://example.com"}, true}, {Station{Name: "x", URL: "file:///tmp/a"}, true}} {
		err := ValidateStation(tc.s)
		if (err != nil) != tc.wantErr {
			t.Fatalf("ValidateStation(%+v) error=%v", tc.s, err)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	if retryDelay(1) != time.Second || retryDelay(2) != 2*time.Second || retryDelay(20) != 30*time.Second {
		t.Fatal("unexpected backoff")
	}
}

func TestStopCancelsStreamAndSwitchCancelsPreviousSession(t *testing.T) {
	oldStarted := make(chan struct{})
	oldCanceled := make(chan struct{})
	newStarted := make(chan struct{})
	newCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "compressed")
		w.(http.Flusher).Flush()
		if r.URL.Path == "/old" {
			close(oldStarted)
			<-r.Context().Done()
			close(oldCanceled)
			return
		}
		close(newStarted)
		<-r.Context().Done()
		close(newCanceled)
	}))
	t.Cleanup(server.Close)

	player := NewPlayer(newTestStreamOpener(server.Client()), testDecoders(t), testAudioOutput{})
	t.Cleanup(func() { _ = player.Close() })
	player.Play(Station{ID: "old", Name: "Old", URL: server.URL + "/old"})
	waitSignal(t, oldStarted, "old station request")
	waitState(t, player, StatePlaying)
	player.Play(Station{ID: "new", Name: "New", URL: server.URL + "/new"})
	waitSignal(t, oldCanceled, "old request cancellation")
	waitSignal(t, newStarted, "new station request")
	player.Stop()
	waitSignal(t, newCanceled, "stopped station request cancellation")
	if player.State() != StateStopped {
		t.Fatalf("state after Stop = %v, want stopped", player.State())
	}
	if err := player.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStopCancelsPendingReconnect(t *testing.T) {
	opener := &failingStreamOpener{calls: make(chan int, 2)}
	player := NewPlayer(opener, testDecoders(t), testAudioOutput{})
	player.Play(Station{ID: "radio", Name: "Radio", URL: "http://radio.invalid/live"})
	select {
	case count := <-opener.calls:
		if count != 1 {
			t.Fatalf("initial open count = %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("initial connection was not attempted")
	}
	deadline := time.After(time.Second)
	for player.State() != StateReconnecting {
		select {
		case <-time.After(time.Millisecond):
		case <-deadline:
			t.Fatal("player did not enter reconnecting state")
		}
	}
	player.Stop()
	select {
	case count := <-opener.calls:
		t.Fatalf("connection attempt %d started after Stop", count)
	case <-time.After(1100 * time.Millisecond):
	}
	if player.State() != StateStopped {
		t.Fatalf("state after Stop = %v, want stopped", player.State())
	}
	_ = player.Close()
}

func TestNoRetryAACDecoderFailureLogsCauseAndStopsRetries(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(previous)

	var opens atomic.Int32
	streams := streamOpenerFunc(func(context.Context, string, func(string)) (io.ReadCloser, codec.StreamInfo, error) {
		opens.Add(1)
		return io.NopCloser(strings.NewReader("compressed")), codec.StreamInfo{Codec: "AAC"}, nil
	})
	decoders := codec.NewRegistry()
	if err := decoders.Register("AAC", func(io.Reader) (codec.Decoder, error) {
		return nil, errors.New("invalid ADTS profile 0")
	}); err != nil {
		t.Fatal(err)
	}
	player := NewPlayer(streams, decoders, testAudioOutput{})
	player.Play(Station{ID: "aac-station", Name: "AAC test", URL: "http://example.invalid/live"})
	waitState(t, player, StateError)
	if err := player.Close(); err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); !strings.Contains(got, "invalid ADTS profile 0") || !strings.Contains(got, "AAC test") {
		t.Fatalf("log does not show station and AAC failure cause: %s", got)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("stream open attempts = %d, want 1 for a non-retryable AAC decoder failure", got)
	}
}

type streamOpenerFunc func(context.Context, string, func(string)) (io.ReadCloser, codec.StreamInfo, error)

func (f streamOpenerFunc) Open(ctx context.Context, url string, onMetadata func(string)) (io.ReadCloser, codec.StreamInfo, error) {
	return f(ctx, url, onMetadata)
}

func newTestStreamOpener(client *http.Client) StreamOpener {
	return &testHTTPStreamOpener{client: client}
}

type testHTTPStreamOpener struct{ client *http.Client }

func (o *testHTTPStreamOpener) Open(ctx context.Context, url string, _ func(string)) (io.ReadCloser, codec.StreamInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, codec.StreamInfo{}, err
	}
	return resp.Body, codec.StreamInfo{URL: url, Codec: "MP3", MIMEType: "audio/mpeg"}, nil
}

type failingStreamOpener struct {
	mu    sync.Mutex
	count int
	calls chan int
}

func (o *failingStreamOpener) Open(context.Context, string, func(string)) (io.ReadCloser, codec.StreamInfo, error) {
	o.mu.Lock()
	o.count++
	count := o.count
	o.mu.Unlock()
	o.calls <- count
	return nil, codec.StreamInfo{}, io.ErrUnexpectedEOF
}

func testDecoders(t *testing.T) *codec.Registry {
	t.Helper()
	decoders := codec.NewRegistry()
	if err := decoders.Register("MP3", func(io.Reader) (codec.Decoder, error) {
		return testDecoder{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	return decoders
}

type testDecoder struct{}

func (testDecoder) Read([]byte) (int, error) { return 0, io.EOF }
func (testDecoder) SampleRate() int          { return 44100 }
func (testDecoder) Channels() int            { return 2 }
func (testDecoder) Close() error             { return nil }

type testAudioOutput struct{}

func (testAudioOutput) Play(context.Context, io.Reader, audio.PCMFormat, float64) (audio.Playback, error) {
	return &testPlayback{}, nil
}

type testPlayback struct {
	mu     sync.Mutex
	closed bool
}

func (*testPlayback) SetVolume(float64) {}
func (*testPlayback) Err() error        { return nil }
func (p *testPlayback) IsPlaying() bool { p.mu.Lock(); defer p.mu.Unlock(); return !p.closed }
func (p *testPlayback) Close() error    { p.mu.Lock(); p.closed = true; p.mu.Unlock(); return nil }

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitState(t *testing.T, player *Engine, state PlayerState) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if player.State() == state {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("player state = %v, want %v", player.State(), state)
		case <-time.After(time.Millisecond):
		}
	}
}
