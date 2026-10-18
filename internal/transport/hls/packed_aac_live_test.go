package hls

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
)

// Playback blocks pipe writes in real time. Playlist polling must count that
// elapsed time instead of adding another delay after the audio is consumed.
func TestAACFallbackReloadIncludesPlaybackTime(t *testing.T) {
	for _, tc := range []struct {
		name     string
		playback time.Duration
		want     time.Duration
	}{
		{name: "audio outlasts polling interval", playback: 2 * time.Second, want: 2 * time.Second},
		{name: "fast consumer waits remaining interval", playback: 250 * time.Millisecond, want: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				wire := bytes.Repeat(testADTSFrame([]byte{0x11, 0x22}), 2)
				start := time.Now()
				var reloadAt time.Time
				var segmentRequests int
				client := New(&http.Client{Transport: packedLiveTransport(func(r *http.Request) (*http.Response, error) {
					body := wire
					switch r.URL.Path {
					case "/radio.m3u8":
						reloadAt = time.Now()
						body = []byte("#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXT-X-MEDIA-SEQUENCE:1\n#EXTINF:2,\nnext.aac\n#EXT-X-ENDLIST\n")
					case "/next.aac":
						segmentRequests++
					default:
						return nil, fmt.Errorf("unexpected request: %s", r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
				})})
				mediaURL, _ := url.Parse("https://example.invalid/radio.m3u8")
				current := &playlist.Media{TargetDuration: 2}
				reader, writer := io.Pipe()
				defer reader.Close()
				done := make(chan struct{})
				go func() {
					defer close(done)
					client.runAACSegments(ctx, writer, mediaURL, current, wire, 0, nil, metadataSegment{})
				}()
				synctest.Wait() // The producer is blocked delivering the first audio.
				time.Sleep(tc.playback)
				got, err := io.ReadAll(reader)
				<-done
				if err != nil || !bytes.Equal(got, bytes.Repeat(wire, 2)) {
					t.Fatalf("audio = %x, error = %v", got, err)
				}
				if elapsed := reloadAt.Sub(start); elapsed != tc.want {
					t.Errorf("playlist reload at %v, want %v", elapsed, tc.want)
				}
				if segmentRequests != 1 {
					t.Errorf("next segment downloaded %d times, want 1", segmentRequests)
				}
			})
		})
	}
}

type packedLiveTransport func(*http.Request) (*http.Response, error)

func (f packedLiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAACFallbackReloadCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		client := New(&http.Client{Transport: packedLiveTransport(func(r *http.Request) (*http.Response, error) {
			t.Errorf("playlist requested after cancellation: %s", r.URL)
			return nil, fmt.Errorf("unexpected request")
		})})
		mediaURL, _ := url.Parse("https://example.invalid/radio.m3u8")
		reader, writer := io.Pipe()
		defer reader.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			client.runAACSegments(ctx, writer, mediaURL, &playlist.Media{TargetDuration: 2}, nil, 0, nil, metadataSegment{})
		}()
		synctest.Wait()
		cancel()
		_, err := io.Copy(io.Discard, reader)
		<-done
		if err != context.Canceled {
			t.Fatalf("read error = %v, want context.Canceled", err)
		}
	})
}

func TestAACFallbackReloadFailureKeepsPollingInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var requested []time.Time
		client := New(&http.Client{Transport: packedLiveTransport(func(r *http.Request) (*http.Response, error) {
			requested = append(requested, time.Now())
			status := http.StatusServiceUnavailable
			if len(requested) == 2 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nprevious.aac\n#EXT-X-ENDLIST\n"))}, nil
		})})
		mediaURL, _ := url.Parse("https://example.invalid/radio.m3u8")
		reader, writer := io.Pipe()
		defer reader.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			client.runAACSegments(ctx, writer, mediaURL, &playlist.Media{TargetDuration: 2}, nil, 0, nil, metadataSegment{})
		}()
		if _, err := io.Copy(io.Discard, reader); err != nil {
			t.Fatal(err)
		}
		<-done
		if len(requested) != 2 || requested[1].Sub(requested[0]) != time.Second {
			t.Fatalf("playlist request times = %v, want two requests one second apart", requested)
		}
	})
}
