package hls

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
)

type metadataSegmentRef struct {
	source string
	epoch  int
}

// metadataTransport observes complete responses from the existing HLS client.
// Audio bytes, headers, redirects, range requests, and HTTP errors are preserved.
// The wrapper never issues a request of its own.
type metadataTransport struct {
	base     http.RoundTripper
	metadata *hlsMetadata
	mu       sync.Mutex
	segments map[string]metadataSegmentRef
	epochs   map[string]int
}

func metadataHTTPClient(base *http.Client, m *hlsMetadata) *http.Client {
	if m == nil {
		return base
	}
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &metadataTransport{base: transport, metadata: m,
		segments: make(map[string]metadataSegmentRef), epochs: make(map[string]int)}
	return &client
}

func (t *metadataTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && (response.StatusCode == http.StatusOK || response.StatusCode == http.StatusPartialContent) {
		response.Body = &metadataBody{ReadCloser: response.Body, complete: func(data []byte) {
			t.observe(request.URL.String(), request.Header.Get("Range"), data)
		}}
	}
	return response, err
}

type metadataBody struct {
	io.ReadCloser
	data     []byte
	tooLarge bool
	complete func([]byte)
}

// Read observes only fully downloaded bodies. Partial responses and oversized
// segments are ignored by diagnostics without affecting their original reader.
func (b *metadataBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if !b.tooLarge {
		if len(b.data)+n > maxPackedSegmentBytes {
			b.data, b.tooLarge = nil, true
		} else {
			b.data = append(b.data, p[:n]...)
		}
	}
	if err == io.EOF && b.complete != nil {
		if !b.tooLarge {
			b.complete(b.data)
		}
		b.complete, b.data = nil, nil
	}
	return n, err
}

func (t *metadataTransport) observe(rawURL, byteRange string, data []byte) {
	if t.metadata.ctx.Err() != nil {
		return
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("#EXTM3U")) {
		parsed, err := unmarshalAACPlaylist(data)
		if err != nil {
			return
		}
		media, ok := parsed.(*playlist.Media)
		base, parseErr := url.Parse(rawURL)
		if !ok || parseErr != nil {
			return
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		if len(t.segments) > 512 {
			clear(t.segments)
		}
		epoch := 0
		if media.DiscontinuitySequence != nil {
			epoch = *media.DiscontinuitySequence
		}
		for _, segment := range media.Segments {
			if segment.Discontinuity {
				epoch++
			}
			if u, err := base.Parse(segment.URI); err == nil {
				t.segments[metadataRequestKey(u.String(), segment.ByteRangeStart, segment.ByteRangeLength)] = metadataSegmentRef{rawURL, epoch}
			}
			for _, part := range segment.Parts {
				if u, err := base.Parse(part.URI); err == nil {
					t.segments[metadataRequestKey(u.String(), part.ByteRangeStart, part.ByteRangeLength)] = metadataSegmentRef{rawURL, epoch}
				}
			}
		}
		return
	}
	if !isMPEGTS(data) && !bytes.HasPrefix(data, []byte("ID3")) {
		return
	}
	t.mu.Lock()
	ref, found := t.segments[rawURL+"\n"+byteRange]
	reset := false
	if found {
		previous, seen := t.epochs[ref.source]
		reset = seen && previous != ref.epoch
		t.epochs[ref.source] = ref.epoch
		delete(t.segments, rawURL+"\n"+byteRange)
	}
	t.mu.Unlock()
	t.metadata.observe(data, reset)
}

func metadataRequestKey(rawURL string, start, length *uint64) string {
	if length == nil {
		return rawURL + "\n"
	}
	offset := uint64(0)
	if start != nil {
		offset = *start
	}
	return rawURL + "\n" + fmt.Sprintf("bytes=%d-%d", offset, offset+*length-1)
}
