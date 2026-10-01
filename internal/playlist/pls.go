package playlist

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ParsePLS parses the FileN and optional TitleN fields in a PLS playlist.
func ParsePLS(r io.Reader) (Playlist, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 512<<10)
	entries := make(map[int]*Entry)
	seen := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if lineNumber == 1 {
			line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		}
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		if key == "" {
			continue
		}
		seen = true
		kind, suffix := "", ""
		for _, prefix := range []string{"file", "title"} {
			if strings.HasPrefix(key, prefix) {
				kind, suffix = prefix, strings.TrimPrefix(key, prefix)
				break
			}
		}
		if kind == "" {
			continue
		}
		index, err := strconv.Atoi(suffix)
		if err != nil || index < 1 {
			continue
		}
		entry := entries[index]
		if entry == nil {
			entry = &Entry{}
			entries[index] = entry
		}
		if kind == "file" {
			entry.URL = value
		} else {
			entry.Title = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Playlist{}, fmt.Errorf("scan PLS playlist: %w", err)
	}
	if !seen && len(entries) == 0 {
		return Playlist{}, ErrEmpty
	}
	indices := make([]int, 0, len(entries))
	for index := range entries {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	result := Playlist{Entries: make([]Entry, 0, len(indices))}
	for _, index := range indices {
		if strings.TrimSpace(entries[index].URL) != "" {
			result.Entries = append(result.Entries, *entries[index])
		}
	}
	if len(result.Entries) == 0 {
		return Playlist{}, ErrNoValidEntries
	}
	return result, nil
}
