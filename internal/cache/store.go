// Package cache provides a small persistent cache for non-sensitive app data.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
	"time"

	"fyne.io/fyne/v2"
)

type Store struct {
	cache fyne.Cache
	now   func() time.Time
	mu    sync.Mutex
}

type Result struct {
	Found     bool
	Fresh     bool
	UpdatedAt time.Time
}

type entry struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Value     json.RawMessage `json:"value"`
}

func New(cache fyne.Cache) *Store {
	return &Store{cache: cache, now: time.Now}
}

// Get decodes a cached value into destination. Expired entries are returned
// with Found=true and Fresh=false so callers can use stale data if needed.
func (s *Store) Get(key string, maxAge time.Duration, destination any) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := cacheName(key)
	if s.cache == nil || !s.cache.Exists(name) {
		return Result{}, nil
	}
	reader, err := s.cache.Read(name)
	if err != nil {
		return Result{}, err
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return Result{}, readErr
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	var cached entry
	if err := json.Unmarshal(data, &cached); err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(cached.Value, destination); err != nil {
		return Result{}, err
	}
	age := s.now().Sub(cached.UpdatedAt)
	return Result{Found: true, Fresh: age >= 0 && age < maxAge, UpdatedAt: cached.UpdatedAt}, nil
}

func (s *Store) Set(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(entry{UpdatedAt: s.now(), Value: data})
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		return nil
	}
	name := cacheName(key)
	writer, err := s.cache.Write(name)
	if err != nil {
		return err
	}
	if _, err := writer.Write(encoded); err != nil {
		_ = writer.Close()
		_ = s.cache.Remove(name)
		return err
	}
	return writer.Close()
}

func cacheName(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}
