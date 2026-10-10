package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

// TestStartupStations checks the startup screen, persistent cache, and offline fallback.
func TestStartupStations(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cached  bool
		age     time.Duration
		failure bool
	}{
		{name: "fresh cache", cached: true, age: time.Hour},
		{name: "expired cache refresh", cached: true, age: 25 * time.Hour},
		{name: "expired cache offline", cached: true, age: 25 * time.Hour, failure: true},
		{name: "no cache"},
		{name: "no cache offline", failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := test.NewTempApp(t)
			app.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
			driver := &startupTestDriver{Driver: app.Driver(), completed: make(chan func(), 1)}
			wrapped := &startupTestApp{App: app, driver: driver}
			fyne.SetCurrentApp(wrapped)
			t.Cleanup(func() { fyne.SetCurrentApp(app) })
			gate := make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-gate:
				default:
					close(gate)
				}
			})
			directory := &startupTestDirectory{gate: gate, stations: []radio.Station{{ID: "new", Name: "New station"}}}
			if tc.failure {
				directory.err = errors.New("network unavailable")
			}
			w := &Window{app: wrapped, window: test.NewTempWindow(t, nil), directory: directory}
			w.settings.DiscoveryCountry = "*"
			w.home = newHomeSection(w)
			w.appContent = w.home.Container
			query, popular := radioStationQuery("*")
			key := radioStationCacheKey(query, popular)
			if tc.cached {
				seedCacheRecord(t, app.Cache(), key, time.Now().Add(-tc.age), []radio.Station{{ID: "cached", Name: "Cached station"}})
			}
			w.showStartup()
			fresh := tc.cached && tc.age < radioCacheTTL
			if w.window.Content() == w.appContent {
				t.Fatal("startup must always begin with the loading screen")
			}
			if !fresh {
				close(gate)
			}
			waitStartup(t, driver.completed)
			if fresh {
				if directory.calls.Load() != 0 {
					t.Fatal("fresh cache must open the app without a request")
				}
			} else {
				if directory.calls.Load() != 1 {
					t.Fatal("startup must make one request")
				}
			}
			if tc.failure && !tc.cached {
				if w.window.Content() == w.appContent {
					t.Fatal("startup without data must remain on the loading screen")
				}
				retry := findOnboardingButton(w.window.Content(), "Retry")
				if retry == nil || !retry.Visible() {
					t.Fatal("startup without data must offer Retry")
				}
				directory.err = nil
				test.Tap(retry)
				waitStartup(t, driver.completed)
				if directory.calls.Load() != 2 || w.window.Content() != w.appContent {
					t.Fatal("successful retry must open the app")
				}
			}
			want := "new"
			if fresh || (tc.cached && tc.failure) {
				want = "cached"
			}
			if w.window.Content() != w.appContent || len(w.home.rows.entries) != 1 || w.home.rows.entries[0].station.ID != want {
				t.Fatalf("startup must open the app with station %q", want)
			}
			var saved []radio.Station
			result, err := cache.New(app.Cache()).Get(key, radioCacheTTL, &saved)
			if err != nil || !result.Found || len(saved) != 1 || saved[0].ID != want {
				t.Fatalf("saved stations = %v, result = %+v, error = %v", saved, result, err)
			}
			if result.Fresh != !(tc.cached && tc.failure) {
				t.Fatal("failed refresh must preserve the expired record's timestamp")
			}
		})
	}
}

// seedCacheRecord writes a dated record through the same backend used by the app.
func seedCacheRecord(t *testing.T, backend fyne.Cache, key string, updated time.Time, value any) {
	t.Helper()
	data, err := json.Marshal(struct {
		UpdatedAt time.Time `json:"updated_at"`
		Value     any       `json:"value"`
	}{updated, value})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256([]byte(key))
	writer, err := backend.Write(hex.EncodeToString(hash[:]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

// waitStartup runs the queued UI update after the background request completes.
func waitStartup(t *testing.T, completed <-chan func()) {
	t.Helper()
	select {
	case fn := <-completed:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("startup did not complete")
	}
}

type startupTestApp struct {
	fyne.App
	driver *startupTestDriver
}

// Driver exposes the queue for startup UI callbacks.
func (a *startupTestApp) Driver() fyne.Driver { return a.driver }

type startupTestDriver struct {
	fyne.Driver
	completed chan func()
}

// DoFromGoroutine queues the UI callback for execution on the test's thread.
func (d *startupTestDriver) DoFromGoroutine(fn func(), _ bool) {
	d.completed <- fn
}

type startupTestDirectory struct {
	gate     <-chan struct{}
	calls    atomic.Int32
	stations []radio.Station
	err      error
}

// Popular holds the request open so the test can inspect the loading screen.
func (d *startupTestDirectory) Popular(ctx context.Context, _ int) ([]radio.Station, error) {
	d.calls.Add(1)
	select {
	case <-d.gate:
		return d.stations, d.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Search uses the same controlled result as the global popularity request.
func (d *startupTestDirectory) Search(ctx context.Context, _ radiobrowser.Query) ([]radio.Station, error) {
	return d.Popular(ctx, 0)
}

// Countries is unused by startup.
func (d *startupTestDirectory) Countries(context.Context) ([]radiobrowser.Country, error) {
	return nil, nil
}
