package icy

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestReaderSeparatesAudioAndMetadata(t *testing.T) {
	var source bytes.Buffer
	source.WriteString("abcd")
	metadata := []byte("StreamTitle='Foo Fighters - Everlong';")
	padded := make([]byte, 48)
	copy(padded, metadata)
	source.WriteByte(byte(len(padded) / 16))
	source.Write(padded)
	source.WriteString("efgh")
	var title string
	reader := NewReader(&source, 4, func(value string) { title = value })
	audio, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "abcdefgh" {
		t.Fatalf("audio = %q", audio)
	}
	if title != "Foo Fighters - Everlong" {
		t.Fatalf("title = %q", title)
	}
}

func TestReaderHandlesEmptyMetadataAndSmallReads(t *testing.T) {
	var source bytes.Buffer
	source.WriteString("abc")
	source.WriteByte(0)
	source.WriteString("def")
	var calls int
	reader := NewReader(&source, 3, func(string) { calls++ })
	var got bytes.Buffer
	buf := make([]byte, 2)
	for {
		n, err := reader.Read(buf)
		got.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if got.String() != "abcdef" || calls != 0 {
		t.Fatalf("audio=%q metadata callbacks=%d", got.String(), calls)
	}
}

func TestReaderReturnsErrorForTruncatedMetadata(t *testing.T) {
	source := bytes.NewReader([]byte{'a', 1, 'x'})
	reader := NewReader(source, 1, nil)
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("expected truncated metadata error")
	}
}

func TestReaderLogsDistinctRawMetadataBlocks(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	var source bytes.Buffer
	block := make([]byte, 48)
	copy(block, "StreamUrl='https://example.invalid/live';")
	for range 2 {
		source.WriteString("abcd")
		source.WriteByte(byte(len(block) / 16))
		source.Write(block)
	}
	clear(block)
	copy(block, "StreamTitle='Artist - Song';")
	source.WriteString("efgh")
	source.WriteByte(byte(len(block) / 16))
	source.Write(block)
	var titles []string
	reader := NewReader(&source, 4, func(title string) { titles = append(titles, title) })
	audio, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "abcdabcdefgh" || len(titles) != 1 || titles[0] != "Artist - Song" {
		t.Fatalf("audio = %q, titles = %q", audio, titles)
	}
	if got := strings.Count(logs.String(), "ICY metadata block received"); got != 2 {
		t.Fatalf("distinct block log count = %d, want 2; logs: %s", got, logs.String())
	}
	if !strings.Contains(logs.String(), "StreamUrl") {
		t.Fatalf("raw unsupported field missing from diagnostics: %s", logs.String())
	}
}
