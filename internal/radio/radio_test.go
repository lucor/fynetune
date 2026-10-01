package radio

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"go.lucor.dev/fynetune/internal/audio"
	"go.lucor.dev/fynetune/internal/codec"
	"go.lucor.dev/fynetune/internal/media"
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

func TestParseTrackMetadata(t *testing.T) {
	got := ParseTrackMetadata(" Foo Fighters - Everlong ")
	if got.RawTitle != "Foo Fighters - Everlong" || got.Artist != "Foo Fighters" || got.Title != "Everlong" || !got.Parsed {
		t.Fatalf("metadata = %+v", got)
	}
	got = ParseTrackMetadata("Station identification")
	if got.RawTitle != "Station identification" || got.Artist != "" || got.Title != got.RawTitle || got.Parsed {
		t.Fatalf("unstructured metadata = %+v", got)
	}
}

func TestMetadataLearningIsPerStation(t *testing.T) {
	player := NewPlayer(nil, nil, nil)
	defer player.Close()

	stationA := Station{ID: "station-a", Name: "A", URL: "https://example.com/a"}
	stationB := Station{ID: "station-b", Name: "B", URL: "https://example.com/b"}
	for _, title := range []string{"A1 ~ One", "A2 ~ Two", "A3 ~ Three", "A4 ~ Four"} {
		player.parseMetadata(stationA, title)
	}
	for _, title := range []string{"B1 - One", "B2 - Two", "B3 - Three", "B4 - Four"} {
		player.parseMetadata(stationB, title)
	}

	gotA := player.parseMetadata(stationA, "Pink Floyd ~ Another Brick - Part 2")
	if !gotA.Parsed || gotA.Artist != "Pink Floyd" || gotA.Title != "Another Brick - Part 2" {
		t.Fatalf("station A metadata after reconnect = %+v", gotA)
	}
	gotB := player.parseMetadata(stationB, "AC-DC - Thunderstruck ~ Live")
	if !gotB.Parsed || gotB.Artist != "AC-DC" || gotB.Title != "Thunderstruck ~ Live" {
		t.Fatalf("station B metadata = %+v", gotB)
	}
}

func TestMetadataLearningSurvivesStreamReconnect(t *testing.T) {
	station := Station{ID: "station-a", Name: "A", URL: "https://example.com/a"}
	opener := &metadataSequenceOpener{
		sequences: [][]string{
			{"A ~ One", "B ~ Two", "C ~ Three", "D ~ Four"},
			{"Pink Floyd ~ Another Brick - Part 2"},
		},
		calls: make(chan int, 2),
	}
	player := NewPlayer(opener, nil, nil)
	player.mu.Lock()
	player.session = 1
	player.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		player.run(ctx, 1, station)
	}()
	defer func() {
		cancel()
		waitSignal(t, done, "player run cancellation")
		_ = player.Close()
	}()

	for want := 1; want <= 2; want++ {
		select {
		case call := <-opener.calls:
			if call != want {
				t.Fatalf("stream open call = %d, want %d", call, want)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("stream open call %d did not occur", want)
		}
	}

	got := player.parseMetadata(station, "Later Artist ~ Title - Remix")
	if !got.Parsed || got.Artist != "Later Artist" || got.Title != "Title - Remix" {
		t.Fatalf("metadata after reconnect = %+v", got)
	}
}

func TestRetryDelay(t *testing.T) {
	if RetryDelay(1) != time.Second || RetryDelay(2) != 2*time.Second || RetryDelay(20) != 30*time.Second {
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
	defer server.Close()

	player := NewPlayer(newTestStreamOpener(server.Client()), testDecoders(t), testAudioOutput{})
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

func newTestStreamOpener(client *http.Client) StreamOpener {
	return &testHTTPStreamOpener{client: client}
}

type testHTTPStreamOpener struct{ client *http.Client }

func (o *testHTTPStreamOpener) Open(ctx context.Context, url string, _ func(string)) (io.ReadCloser, media.StreamInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, media.StreamInfo{}, err
	}
	return resp.Body, media.StreamInfo{URL: url, Codec: "MP3", MIMEType: "audio/mpeg"}, nil
}

type failingStreamOpener struct {
	mu    sync.Mutex
	count int
	calls chan int
}

type metadataSequenceOpener struct {
	mu        sync.Mutex
	attempt   int
	sequences [][]string
	calls     chan int
}

func (o *metadataSequenceOpener) Open(_ context.Context, _ string, onMetadata func(string)) (io.ReadCloser, media.StreamInfo, error) {
	o.mu.Lock()
	attempt := o.attempt
	o.attempt++
	var sequence []string
	if attempt < len(o.sequences) {
		sequence = append(sequence, o.sequences[attempt]...)
	}
	o.mu.Unlock()
	for _, title := range sequence {
		onMetadata(title)
	}
	o.calls <- attempt + 1
	return nil, media.StreamInfo{}, io.ErrUnexpectedEOF
}

func (o *failingStreamOpener) Open(context.Context, string, func(string)) (io.ReadCloser, media.StreamInfo, error) {
	o.mu.Lock()
	o.count++
	count := o.count
	o.mu.Unlock()
	o.calls <- count
	return nil, media.StreamInfo{}, io.ErrUnexpectedEOF
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
