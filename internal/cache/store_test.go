package cache

import (
	"bytes"
	"io"
	"testing"
	"time"

	"fyne.io/fyne/v2"
)

type memoryCache map[string][]byte

func (m memoryCache) RootURI() fyne.URI { return nil }
func (m memoryCache) Exists(name string) bool {
	_, ok := m[name]
	return ok
}
func (m memoryCache) Read(name string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m[name])), nil
}
func (m memoryCache) Write(name string) (io.WriteCloser, error) {
	return &memoryCacheWriter{Buffer: &bytes.Buffer{}, cache: m, name: name}, nil
}
func (m memoryCache) Remove(name string) error {
	delete(m, name)
	return nil
}

type memoryCacheWriter struct {
	*bytes.Buffer
	cache memoryCache
	name  string
}

func (w *memoryCacheWriter) Close() error {
	w.cache[w.name] = append([]byte(nil), w.Bytes()...)
	return nil
}

func TestStoreReturnsFreshAndExpiredValues(t *testing.T) {
	cache := make(memoryCache)
	store := New(cache)
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }

	want := []string{"one", "two"}
	if err := store.Set("popular", want); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	var got []string
	result, err := store.Get("popular", 24*time.Hour, &got)
	if err != nil || !result.Found || !result.Fresh {
		t.Fatalf("Get() result = %+v, error = %v", result, err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Get() value = %v, want %v", got, want)
	}

	now = now.Add(25 * time.Hour)
	result, err = store.Get("popular", 24*time.Hour, &got)
	if err != nil || !result.Found || result.Fresh {
		t.Fatalf("expired Get() result = %+v, error = %v", result, err)
	}
}

func TestStoreMissingKey(t *testing.T) {
	store := New(make(memoryCache))
	var got []string
	result, err := store.Get("missing", time.Hour, &got)
	if err != nil || result.Found || result.Fresh {
		t.Fatalf("Get() result = %+v, error = %v", result, err)
	}
}
