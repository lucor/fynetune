package hls

import (
	"context"
	"io"
	"sync"
)

// packedAACStream exposes downloaded packed-AAC segments as a reader and stops its worker on Close.
type packedAACStream struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// Read reads packed AAC bytes produced by the segment worker.
func (s *packedAACStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

// Close cancels the segment worker, closes the pipe, and waits for worker exit.
func (s *packedAACStream) Close() error {
	s.once.Do(func() {
		s.cancel()
		_ = s.reader.Close()
		_ = s.writer.CloseWithError(context.Canceled)
	})
	<-s.done
	return nil
}
