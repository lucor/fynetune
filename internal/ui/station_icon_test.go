package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
)

// TestStationIconPersistence checks bounded PNG storage, unchanged SVG sources,
// and disk reuse by a new station list with no icons in memory.
func TestStationIconPersistence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source image.Point
		want   image.Point
	}{
		{name: "small", source: image.Pt(32, 24), want: image.Pt(32, 24)},
		{name: "at limit", source: image.Pt(256, 128), want: image.Pt(256, 128)},
		{name: "landscape", source: image.Pt(1024, 512), want: image.Pt(256, 128)},
		{name: "portrait", source: image.Pt(128, 768), want: image.Pt(42, 256)},
		{name: "svg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := test.NewTempApp(t)
			payload := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512"><rect width="512" height="512" fill="red"/></svg>`)
			pixel := color.NRGBA{R: 24, G: 64, B: 128, A: 255}
			if tc.name != "svg" {
				source := image.NewNRGBA(image.Rectangle{Max: tc.source})
				source.SetNRGBA(0, 0, pixel)
				var data bytes.Buffer
				if err := png.Encode(&data, source); err != nil {
					t.Fatal(err)
				}
				payload = data.Bytes()
				if tc.source.X <= stationIconSize && tc.source.Y <= stationIconSize && resizeStationIcon(source) != source {
					t.Fatal("small rasters must be retained without resampling")
				}
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			station := radio.Station{Favicon: server.URL}
			key := stationIconKey(station)
			list := &stationList{window: &Window{app: app}, iconClient: server.Client()}
			icon, err := list.loadStationIcon(context.Background(), station, key)
			if err != nil {
				t.Fatal(err)
			}
			var saved stationIconRecord
			result, err := cache.New(app.Cache()).Get("station-icon:"+key, stationIconTTL, &saved)
			if err != nil || !result.Fresh {
				t.Fatalf("saved icon result = %+v, error = %v", result, err)
			}
			stored, err := favicon.Decode(saved.Data)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "svg" {
				if !bytes.Equal(icon.SVG, payload) || !bytes.Equal(saved.Data, payload) {
					t.Fatal("SVG sources must remain unchanged")
				}
			} else {
				if stored.Format != "png" || stored.Raster.Bounds().Size() != tc.want || icon.Raster.Bounds().Size() != tc.want {
					t.Fatalf("raster dimensions = %v, stored = %v, want %v", icon.Raster.Bounds(), stored.Raster.Bounds(), tc.want)
				}
				if tc.name == "small" && color.NRGBAModel.Convert(stored.Raster.At(0, 0)) != pixel {
					t.Fatal("small raster pixels must survive PNG storage unchanged")
				}
			}
			// A fresh list uses the persisted data, without a second HTTP request.
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })
			restarted := &stationList{window: &Window{app: app}, iconClient: server.Client()}
			icon, err = restarted.loadStationIcon(context.Background(), station, key)
			if err != nil || calls.Load() != 1 {
				t.Fatalf("restart error = %v, requests = %d", err, calls.Load())
			}
			if logs.Len() != 0 {
				t.Fatalf("fresh icon cache reuse should be silent: %s", logs.String())
			}
			if tc.name == "svg" {
				if !bytes.Equal(icon.SVG, payload) {
					t.Fatal("restart must restore the SVG source")
				}
			} else if icon.Raster.Bounds().Size() != tc.want {
				t.Fatal("restart must restore the stored raster dimensions")
			}
		})
	}
}

// TestStationIconRefresh checks expiration, offline fallback, and invalid-cache recovery.
func TestStationIconRefresh(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cached  bool
		invalid bool
		failure bool
	}{
		{name: "expired", cached: true},
		{name: "expired offline", cached: true, failure: true},
		{name: "missing offline", failure: true},
		{name: "invalid", cached: true, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := test.NewTempApp(t)
			old := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`)
			fresh := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="blue"/></svg>`)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if tc.failure {
					http.Error(w, "unavailable", http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write(fresh)
			}))
			defer server.Close()
			station := radio.Station{Favicon: server.URL}
			key := stationIconKey(station)
			if tc.cached {
				updated := time.Now().Add(-31 * 24 * time.Hour)
				data := old
				if tc.invalid {
					data = []byte("invalid image")
					updated = time.Now()
				}
				seedCacheRecord(t, app.Cache(), "station-icon:"+key, updated, stationIconRecord{Data: data, URL: server.URL})
			}
			list := &stationList{window: &Window{app: app}, iconClient: server.Client()}
			icon, err := list.loadStationIcon(context.Background(), station, key)
			if calls.Load() != 1 {
				t.Fatal("a missing, invalid, or expired record must trigger one download")
			}
			if tc.failure && !tc.cached {
				if err == nil {
					t.Fatal("download failure without a cached image must return an error")
				}
				return
			}
			want := fresh
			if tc.failure {
				want = old
			}
			if err != nil || !bytes.Equal(icon.SVG, want) {
				t.Fatalf("icon = %q, error = %v", icon.SVG, err)
			}
			var saved stationIconRecord
			result, err := cache.New(app.Cache()).Get("station-icon:"+key, stationIconTTL, &saved)
			if err != nil || !bytes.Equal(saved.Data, want) || result.Fresh == tc.failure {
				t.Fatalf("saved = %+v, result = %+v, error = %v", saved, result, err)
			}
		})
	}
}

// TestStationIconDiscovery checks Radio Browser priority and revalidation of
// the saved homepage icon without fetching the homepage again.
func TestStationIconDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		direct string
	}{
		{name: "direct", direct: "valid"},
		{name: "direct HTTP error", direct: "error"},
		{name: "direct invalid image", direct: "invalid"},
		{name: "missing direct URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := test.NewTempApp(t)
			payload := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="blue"/></svg>`)
			var directCalls, homeCalls, discoveredCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("If-None-Match") != "" {
					if r.Header.Get("If-None-Match") != `"icon-v1"` {
						t.Error("validation must use the saved ETag")
					}
					if r.URL.Path == "/direct" {
						directCalls.Add(1)
					} else if r.URL.Path == "/discovered" {
						discoveredCalls.Add(1)
					} else {
						t.Error("validators must target the image, not the homepage")
					}
					w.WriteHeader(http.StatusNotModified)
					return
				}
				switch r.URL.Path {
				case "/direct":
					directCalls.Add(1)
					if tc.direct == "error" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					if tc.direct == "invalid" {
						_, _ = w.Write([]byte("invalid image"))
						return
					}
				case "/home":
					homeCalls.Add(1)
					_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/discovered"></head></html>`))
					return
				case "/discovered":
					discoveredCalls.Add(1)
				default:
					http.NotFound(w, r)
					return
				}
				w.Header().Set("ETag", `"icon-v1"`)
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			station := radio.Station{Homepage: server.URL + "/home"}
			if tc.direct != "" {
				station.Favicon = server.URL + "/direct"
			}
			key := stationIconKey(station)
			list := &stationList{window: &Window{app: app}, iconClient: server.Client()}
			if _, err := list.loadStationIcon(context.Background(), station, key); err != nil {
				t.Fatal(err)
			}
			var saved stationIconRecord
			store := cache.New(app.Cache())
			if _, err := store.Get("station-icon:"+key, stationIconTTL, &saved); err != nil {
				t.Fatal(err)
			}
			wantPath := "/discovered"
			wantHome := int32(1)
			if tc.direct == "valid" {
				wantPath = "/direct"
				wantHome = 0
			}
			if saved.URL != server.URL+wantPath || saved.ETag != `"icon-v1"` || !bytes.Equal(saved.Data, payload) || homeCalls.Load() != wantHome {
				t.Fatalf("saved = %+v, homepage requests = %d", saved, homeCalls.Load())
			}
			beforeDirect, beforeDiscovered := directCalls.Load(), discoveredCalls.Load()
			updated := time.Now().Add(-8 * 24 * time.Hour)
			seedCacheRecord(t, app.Cache(), "station-icon:"+key, updated, saved)
			icon, err := list.loadStationIcon(context.Background(), station, key)
			if err != nil || !bytes.Equal(icon.SVG, payload) {
				t.Fatalf("validation icon = %+v, error = %v", icon, err)
			}
			wantDirect, wantDiscovered := beforeDirect, beforeDiscovered
			if tc.direct == "valid" {
				wantDirect++
			} else {
				wantDiscovered++
			}
			if homeCalls.Load() != wantHome || directCalls.Load() != wantDirect || discoveredCalls.Load() != wantDiscovered {
				t.Fatal("validation must fetch only the saved image URL")
			}
			result, err := store.Get("station-icon:"+key, stationIconTTL, &saved)
			if err != nil || !result.UpdatedAt.After(updated) || saved.ETag != `"icon-v1"` || !bytes.Equal(saved.Data, payload) {
				t.Fatalf("304 cache = %+v, result = %+v, error = %v", saved, result, err)
			}
			if _, err := list.loadStationIcon(context.Background(), station, key); err != nil {
				t.Fatal(err)
			}
			if homeCalls.Load() != wantHome || directCalls.Load() != wantDirect || discoveredCalls.Load() != wantDiscovered {
				t.Fatal("a successful 304 must renew the cache without another request")
			}
		})
	}
}

// TestStationIconValidation checks both TTLs, new images, offline preservation,
// and unconditional recovery when cached pixels cannot be decoded.
func TestStationIconValidation(t *testing.T) {
	const modified = "Tue, 06 Oct 2026 12:00:00 GMT"
	for _, tc := range []struct {
		name         string
		age          time.Duration
		etag         string
		lastModified string
		status       int
		requests     int32
		invalid      bool
	}{
		{name: "fresh etag", age: 6 * 24 * time.Hour, etag: `"old"`},
		{name: "fresh without validators", age: 8 * 24 * time.Hour},
		{name: "etag unchanged", age: 8 * 24 * time.Hour, etag: `"old"`, status: http.StatusNotModified, requests: 1},
		{name: "last modified unchanged", age: 8 * 24 * time.Hour, lastModified: modified, status: http.StatusNotModified, requests: 1},
		{name: "updated", age: 8 * 24 * time.Hour, etag: `"old"`, status: http.StatusOK, requests: 1},
		{name: "expired without validators", age: 31 * 24 * time.Hour, status: http.StatusOK, requests: 1},
		{name: "offline", age: 8 * 24 * time.Hour, etag: `"old"`, status: http.StatusServiceUnavailable, requests: 1},
		{name: "corrupt pixels", age: time.Hour, etag: `"old"`, status: http.StatusOK, requests: 1, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := test.NewTempApp(t)
			old := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`)
			fresh := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="blue"/></svg>`)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.requests == 0 {
					t.Error("fresh cache must not make an HTTP request")
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				wantETag, wantModified := tc.etag, tc.lastModified
				if tc.invalid {
					wantETag, wantModified = "", ""
				}
				if r.Header.Get("If-None-Match") != wantETag || r.Header.Get("If-Modified-Since") != wantModified {
					t.Errorf("validators = %v", r.Header)
				}
				if tc.status == http.StatusOK {
					w.Header().Set("ETag", `"new"`)
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusOK {
					_, _ = w.Write(fresh)
				}
			}))
			defer server.Close()
			station := radio.Station{Favicon: server.URL}
			key := stationIconKey(station)
			updated := time.Now().Add(-tc.age)
			record := stationIconRecord{Data: old, URL: server.URL, ETag: tc.etag, LastModified: tc.lastModified}
			if tc.invalid {
				record.Data = []byte("invalid")
			}
			seedCacheRecord(t, app.Cache(), "station-icon:"+key, updated, record)
			list := &stationList{window: &Window{app: app}, iconClient: server.Client()}
			icon, err := list.loadStationIcon(context.Background(), station, key)
			want := old
			if tc.status == http.StatusOK {
				want = fresh
			}
			if err != nil || !bytes.Equal(icon.SVG, want) || calls.Load() != tc.requests {
				t.Fatalf("icon = %+v, error = %v, requests = %d", icon, err, calls.Load())
			}
			var saved stationIconRecord
			result, err := cache.New(app.Cache()).Get("station-icon:"+key, stationIconTTL, &saved)
			if err != nil || !bytes.Equal(saved.Data, want) {
				t.Fatalf("saved = %+v, error = %v", saved, err)
			}
			if tc.status == http.StatusOK {
				if saved.ETag != `"new"` || saved.LastModified != "" {
					t.Fatal("a new image must replace its validators")
				}
			} else if saved.ETag != tc.etag || saved.LastModified != tc.lastModified {
				t.Fatal("unchanged or offline images must retain their validators")
			}
			if tc.status == http.StatusOK || tc.status == http.StatusNotModified {
				if !result.UpdatedAt.After(updated) {
					t.Fatal("successful downloads and validations must renew the timestamp")
				}
			} else if !result.UpdatedAt.Equal(updated) {
				t.Fatal("cache hits and failed validations must retain the original timestamp")
			}
		})
	}
}

// TestStationIconRediscovery checks that a failed saved source falls back to
// Radio Browser without leaking validators to the replacement image.
func TestStationIconRediscovery(t *testing.T) {
	app := test.NewTempApp(t)
	old := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`)
	fresh := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="blue"/></svg>`)
	var oldCalls, directCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			oldCalls.Add(1)
			if r.Header.Get("If-None-Match") != `"old"` {
				t.Error("saved source must receive its validator")
			}
			http.NotFound(w, r)
		case "/direct":
			directCalls.Add(1)
			if r.Header.Get("If-None-Match") != "" || r.Header.Get("If-Modified-Since") != "" {
				t.Error("replacement source must not receive unrelated validators")
			}
			_, _ = w.Write(fresh)
		default:
			t.Error("homepage must not be requested when the direct URL succeeds")
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	station := radio.Station{Favicon: server.URL + "/direct", Homepage: server.URL + "/home"}
	key := stationIconKey(station)
	seedCacheRecord(t, app.Cache(), "station-icon:"+key, time.Now().Add(-8*24*time.Hour), stationIconRecord{
		Data: old, URL: server.URL + "/old", ETag: `"old"`,
	})
	list := &stationList{window: &Window{app: app}, iconClient: server.Client()}
	icon, err := list.loadStationIcon(context.Background(), station, key)
	if err != nil || !bytes.Equal(icon.SVG, fresh) || oldCalls.Load() != 1 || directCalls.Load() != 1 {
		t.Fatalf("rediscovery icon = %+v, error = %v, requests = %d/%d", icon, err, oldCalls.Load(), directCalls.Load())
	}
	var saved stationIconRecord
	if _, err := cache.New(app.Cache()).Get("station-icon:"+key, stationIconTTL, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.URL != station.Favicon || saved.ETag != "" || !bytes.Equal(saved.Data, fresh) {
		t.Fatalf("replacement record = %+v", saved)
	}
}

// TestStationIconLegacyCache keeps earlier image-only records usable offline.
func TestStationIconLegacyCache(t *testing.T) {
	for _, age := range []time.Duration{time.Hour, 31 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			app := test.NewTempApp(t)
			payload := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red"/></svg>`)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				http.Error(w, "offline", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			station := radio.Station{Favicon: server.URL}
			key := stationIconKey(station)
			seedCacheRecord(t, app.Cache(), "station-icon:"+key, time.Now().Add(-age), payload)
			list := &stationList{window: &Window{app: app}, iconClient: server.Client()}
			icon, err := list.loadStationIcon(context.Background(), station, key)
			wantCalls := int32(0)
			if age > stationIconTTL {
				wantCalls = 1
			}
			if err != nil || !bytes.Equal(icon.SVG, payload) || calls.Load() != wantCalls {
				t.Fatalf("legacy icon = %+v, error = %v, requests = %d", icon, err, calls.Load())
			}
		})
	}
}
