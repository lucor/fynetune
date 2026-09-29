// Package icy strips ICY metadata blocks from an audio stream.
package icy

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

var titlePattern = regexp.MustCompile(`(?i)StreamTitle='([^']*)'`)

type Reader struct {
	r                   *bufio.Reader
	interval, remaining int
	OnTitle             func(string)
}

func NewReader(r io.Reader, interval int, onTitle func(string)) *Reader {
	return &Reader{r: bufio.NewReader(r), interval: interval, remaining: interval, OnTitle: onTitle}
}

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
		length := int(lengthByte) * 16
		if length > 0 {
			block := make([]byte, length)
			if _, err := io.ReadFull(r.r, block); err != nil {
				return 0, err
			}
			if match := titlePattern.FindStringSubmatch(string(block)); len(match) > 1 && r.OnTitle != nil {
				if title := strings.TrimSpace(strings.TrimRight(match[1], "\x00")); title != "" {
					r.OnTitle(title)
				}
			}
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
