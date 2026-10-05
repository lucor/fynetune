package hls

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/bluenviron/gohlslib/v2/pkg/playlist"
)

// These limits bound memory use and playlist recursion in the packed-AAC fallback.
const (
	maxPackedPlaylistBytes = 512 << 10
	maxPackedSegmentBytes  = 8 << 20
	maxPackedPlaylistDepth = 3
)

// errNotPackedAAC lets Open distinguish packed AAC from MPEG-TS decode failures.
var errNotPackedAAC = errors.New("HLS segment is not packed AAC/ADTS")

// selectAACVariant prefers the highest-bandwidth variant that advertises AAC.
func selectAACVariant(variants []*playlist.MultivariantVariant) *playlist.MultivariantVariant {
	var audio []*playlist.MultivariantVariant
	for _, variant := range variants {
		for _, codec := range variant.Codecs {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(codec)), "mp4a.") {
				audio = append(audio, variant)
				break
			}
		}
	}
	if len(audio) == 0 {
		audio = variants
	}
	sort.SliceStable(audio, func(i, j int) bool { return audio[i].Bandwidth > audio[j].Bandwidth })
	if len(audio) == 0 {
		return nil
	}
	return audio[0]
}

// newestPackedSegment starts near the live edge without skipping decoder probe frames.
func newestPackedSegment(pl *playlist.Media) (*playlist.MediaSegment, int) {
	if len(pl.Segments) == 0 {
		return nil, 0
	}
	// Start close to the live edge, while retaining enough AAC frames for the
	// decoder's SBR/PS probe and avoiding a large startup backlog.
	index := max(0, len(pl.Segments)-3)
	return pl.Segments[index], index
}

// waitPackedReload pauses between live playlist requests and remains cancelable.
func waitPackedReload(ctx context.Context, targetDuration int) error {
	delay := time.Duration(targetDuration) * time.Second / 2
	if delay < 250*time.Millisecond {
		delay = 250 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
