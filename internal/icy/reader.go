// Package icy removes ICY metadata blocks from audio streams and parses their track titles.
package icy

import (
	"bufio"
	"io"
	"log/slog"
	"regexp"
	"strings"
)

var titlePattern = regexp.MustCompile(`(?i)StreamTitle='([^']*)'`)

// Reader exposes audio bytes from an ICY stream and reports embedded track titles.
type Reader struct {
	r         *bufio.Reader
	interval  int
	remaining int
	onTitle   func(string)
	lastBlock string
}

// NewReader wraps r and removes one ICY metadata block after each audio interval.
func NewReader(r io.Reader, interval int, onTitle func(string)) *Reader {
	return &Reader{r: bufio.NewReader(r), interval: interval, remaining: interval, onTitle: onTitle}
}

// Read returns audio bytes while consuming metadata blocks at their intervals.
func (r *Reader) Read(p []byte) (int, error) {
	if r.interval <= 0 {
		return r.r.Read(p)
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		lengthByte, err := r.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if err := r.readMetadata(int(lengthByte) * 16); err != nil {
			return 0, err
		}
		r.remaining = r.interval
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.r.Read(p)
	r.remaining -= n
	return n, err
}

// readMetadata consumes a metadata block whose length is measured in 16-byte units.
func (r *Reader) readMetadata(length int) error {
	if length == 0 {
		return nil
	}
	if r.onTitle == nil {
		_, err := io.CopyN(io.Discard, r.r, int64(length))
		if err == io.EOF {
			return io.ErrUnexpectedEOF
		}
		return err
	}
	block := make([]byte, length)
	if _, err := io.ReadFull(r.r, block); err != nil {
		return err
	}
	input := strings.TrimRight(string(block), "\x00 \t\r\n")
	if input != r.lastBlock {
		r.lastBlock = input
		if input != "" {
			slog.Debug("ICY metadata block received", "input", input)
		}
	}
	match := titlePattern.FindSubmatch(block)
	if len(match) < 2 {
		return nil
	}
	title := strings.TrimSpace(strings.TrimRight(string(match[1]), "\x00"))
	if title != "" {
		r.onTitle(title)
	}
	return nil
}
