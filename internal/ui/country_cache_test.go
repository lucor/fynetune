package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/radiobrowser"
)

// TestSettingsCountryCache checks that a cached list opens without a loading modal.
func TestSettingsCountryCache(t *testing.T) {
	for _, cached := range []bool{true, false} {
		name := "missing"
		if cached {
			name = "cached"
		}
		t.Run(name, func(t *testing.T) {
			base := test.NewTempApp(t)
			base.Settings().SetTheme(&brandTheme{Theme: theme.DefaultTheme(), appearance: appearanceLight})
			driver := &startupTestDriver{Driver: base.Driver(), completed: make(chan func(), 1)}
			app := &startupTestApp{App: base, driver: driver}
			fyne.SetCurrentApp(app)
			t.Cleanup(func() { fyne.SetCurrentApp(base) })
			w := &Window{app: app, window: test.NewTempWindow(t, nil), directory: &startupTestDirectory{}}
			w.window.Resize(fyne.NewSize(480, 800))
			settings := widget.NewLabel("Settings")
			w.setContent(settings)
			if cached {
				if err := cache.New(app.Cache()).Set(countryCacheKey, []radiobrowser.Country{{Name: "Italy", Code: "IT"}}); err != nil {
					t.Fatal(err)
				}
			}
			w.changeCountry()
			if cached {
				if w.onboarding.loadingDialog != nil || w.onboarding.list == nil || w.window.Content() == settings {
					t.Fatal("fresh cached countries must open the picker without a loading modal")
				}
			} else {
				if w.onboarding.loadingDialog == nil || w.window.Content() != settings {
					t.Fatal("missing countries must show a loading modal over Settings")
				}
				waitStartup(t, driver.completed)
			}
		})
	}
}

// TestCountryCache checks TTL, refresh, offline fallback, and reuse by a new window.
func TestCountryCache(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cached  bool
		age     time.Duration
		failure bool
	}{
		{name: "fresh", cached: true, age: 6 * 24 * time.Hour},
		{name: "expired", cached: true, age: 8 * 24 * time.Hour},
		{name: "expired offline", cached: true, age: 8 * 24 * time.Hour, failure: true},
		{name: "missing"},
		{name: "missing offline", failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := test.NewTempApp(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/json/countries" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				if tc.failure {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(`[{"name":"Italy","iso_3166_1":"IT"}]`))
			}))
			defer server.Close()
			window := &Window{app: app, directory: radiobrowser.NewWithBaseURL(server.URL, server.Client())}
			if tc.cached {
				seedCacheRecord(t, app.Cache(), countryCacheKey, time.Now().Add(-tc.age), []radiobrowser.Country{{Name: "Japan", Code: "JP"}})
			}
			countries, err := window.loadCountries(context.Background())
			fresh := tc.cached && tc.age < countryCacheTTL
			wantCalls := int32(1)
			if fresh {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Fatalf("requests = %d, want %d", calls.Load(), wantCalls)
			}
			if tc.failure && !tc.cached {
				if err == nil {
					t.Fatal("missing cache and HTTP failure must return an error")
				}
				return
			}
			want := "IT"
			if fresh || tc.failure {
				want = "JP"
			}
			if err != nil || len(countries) != 1 || countries[0].Code != want {
				t.Fatalf("countries = %v, error = %v", countries, err)
			}
			var saved []radiobrowser.Country
			result, err := cache.New(app.Cache()).Get(countryCacheKey, countryCacheTTL, &saved)
			if err != nil || !result.Found || len(saved) != 1 || saved[0].Code != want {
				t.Fatalf("cache = %v, result = %+v, error = %v", saved, result, err)
			}
			if tc.failure {
				if result.Fresh {
					t.Fatal("failed refresh must preserve the expired timestamp")
				}
				return
			}
			// A new window reads the persisted value without contacting the directory.
			restarted := &Window{app: app}
			countries, err = restarted.loadCountries(context.Background())
			if err != nil || len(countries) != 1 || countries[0].Code != want {
				t.Fatalf("restart countries = %v, error = %v", countries, err)
			}
		})
	}
}

// TestCountryCacheConcurrentRequests checks that callers share one successful fetch.
func TestCountryCacheConcurrentRequests(t *testing.T) {
	app := test.NewTempApp(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`[{"name":"Italy","iso_3166_1":"IT"}]`))
	}))
	defer server.Close()
	window := &Window{app: app, directory: radiobrowser.NewWithBaseURL(server.URL, server.Client())}
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			countries, err := window.loadCountries(context.Background())
			if err != nil || len(countries) != 1 || countries[0].Code != "IT" {
				t.Errorf("countries = %v, error = %v", countries, err)
			}
		})
	}
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("requests = %d, want 1", calls.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := window.loadCountries(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request error = %v", err)
	}
}
