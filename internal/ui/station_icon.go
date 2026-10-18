package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/cache"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/radio"
	xdraw "golang.org/x/image/draw"
)

const (
	stationIconSize               = 256
	stationIconTTL                = 30 * 24 * time.Hour
	stationIconValidationInterval = 7 * 24 * time.Hour
)

// stationIconRecord ties the cached pixels to the URL and validators of their source.
type stationIconRecord struct {
	Data         []byte `json:"data"`
	URL          string `json:"url"`
	ETag         string `json:"etag,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
}

// loadStationIcon reads the disk cache before trying the station's icon sources.
// SVG bytes are preserved and rasters are stored as PNG, with no dimension above
// 256 px. The JSON cache record includes a timestamp and base64-encoded image data.
// Validators are checked after seven days; sources without validators refresh
// after 30 days. Station metadata refreshes do not remove these separate records.
// Expired images remain available as a fallback when their refresh fails.
func (s *stationList) loadStationIcon(ctx context.Context, station radio.Station, key string) (favicon.Icon, error) {
	store := cache.New(s.window.app.Cache())
	cacheKey := "station-icon:" + key
	var record stationIconRecord
	result, err := store.Get(cacheKey, stationIconTTL, &record)
	if err != nil {
		// Earlier records stored only image bytes. They remain usable until a
		// successful download adds the source URL and validators.
		var data []byte
		result, err = store.Get(cacheKey, stationIconTTL, &data)
		if err == nil {
			record = stationIconRecord{Data: data}
		}
	}
	if err != nil {
		slog.Warn("could not read cached station icon", "error", err)
		result = cache.Result{}
		record = stationIconRecord{}
	}
	var cached favicon.Icon
	if result.Found {
		cached, err = favicon.Decode(record.Data)
		if err != nil {
			slog.Warn("could not decode cached station icon", "error", err)
			result = cache.Result{}
			record = stationIconRecord{}
		}
		needsValidation := record.URL != "" && (record.ETag != "" || record.LastModified != "") &&
			time.Since(result.UpdatedAt) >= stationIconValidationInterval
		if result.Found && result.Fresh && !needsValidation {
			return cached, nil
		}
	}
	hasCached := result.Found
	download, err := s.downloadStationIcon(ctx, station, record)
	if err != nil {
		if hasCached {
			slog.Debug("station icon refresh failed; using cached icon", "station", station.Name, "error", err)
			return cached, nil
		}
		slog.Debug("station icon unavailable; using placeholder", "station", station.Name, "error", err)
		return favicon.Icon{}, err
	}
	icon := cached
	if !download.NotModified {
		icon = download.Icon
		var outputSize image.Point
		if icon.Raster != nil {
			icon.Raster = resizeStationIcon(icon.Raster)
		}
		if icon.Raster != nil {
			outputSize = icon.Raster.Bounds().Size()
		}
		record.Data, err = encodeStationIcon(icon)
		if err != nil {
			slog.Warn("could not cache station icon", "error", err)
			return icon, nil
		}
		slog.Debug("station icon downloaded",
			"station", station.Name,
			"url", download.URL,
			"format", icon.Format,
			"source_size", icon.Size,
			"stored_size", outputSize,
			"etag", download.ETag,
			"last_modified", download.LastModified,
		)
	} else {
		slog.Debug("station icon unchanged",
			"station", station.Name,
			"url", download.URL,
			"etag", download.ETag,
			"last_modified", download.LastModified,
		)
	}
	record.URL = download.URL
	record.ETag = download.ETag
	record.LastModified = download.LastModified
	// A successful 304 renews the timestamp while preserving the stored image bytes.
	if err := store.Set(cacheKey, record); err != nil {
		slog.Warn("could not cache station icon", "error", err)
	}
	return icon, nil
}

// downloadStationIcon validates a saved source before rediscovering an icon.
// Discovery tries Radio Browser's direct URL first, then the homepage. Validators
// belong only to the saved URL and are never passed to either discovery request.
func (s *stationList) downloadStationIcon(ctx context.Context, station radio.Station, record stationIconRecord) (favicon.DownloadResult, error) {
	opts := favicon.Options{Client: s.iconClient}
	var lastErr error
	if record.URL != "" {
		conditional := opts
		conditional.ETag = record.ETag
		conditional.LastModified = record.LastModified
		result, err := favicon.DownloadURL(ctx, record.URL, conditional)
		if err == nil {
			slog.Debug("station icon source selected", "station", station.Name, "source", "cached URL", "url", result.URL)
			return result, nil
		}
		slog.Debug("station icon source failed", "station", station.Name, "source", "cached URL", "url", record.URL, "error", err)
		lastErr = fmt.Errorf("cached URL %q: %w", record.URL, err)
	}
	// Do not retry the same direct URL if its validation has just failed.
	directURL := strings.TrimSpace(station.Favicon)
	if directURL != "" && directURL != record.URL {
		result, err := favicon.DownloadURL(ctx, directURL, opts)
		if err == nil {
			slog.Debug("station icon source selected", "station", station.Name, "source", "Radio Browser favicon", "url", result.URL)
			return result, nil
		}
		slog.Debug("station icon source failed", "station", station.Name, "source", "Radio Browser favicon", "url", directURL, "error", err)
		lastErr = fmt.Errorf("radio browser favicon %q: %w", directURL, err)
	} else if directURL == "" {
		slog.Debug("station icon source skipped", "station", station.Name, "source", "Radio Browser favicon", "reason", "favicon URL is missing")
	} else {
		slog.Debug("station icon source skipped", "station", station.Name, "source", "Radio Browser favicon", "reason", "same URL was already checked")
	}
	homepage := strings.TrimSpace(station.Homepage)
	if homepage == "" {
		slog.Debug("station icon source skipped", "station", station.Name, "source", "homepage", "reason", "homepage URL is missing")
		if lastErr != nil {
			return favicon.DownloadResult{}, lastErr
		}
		return favicon.DownloadResult{}, fmt.Errorf("station has no favicon URL or homepage")
	}
	parsed, err := url.Parse(homepage)
	if err != nil {
		err = fmt.Errorf("parse station homepage: %w", err)
		slog.Debug("station icon source failed", "station", station.Name, "source", "homepage", "url", homepage, "error", err)
		return favicon.DownloadResult{}, err
	}
	if parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		err = fmt.Errorf("invalid station homepage %q", homepage)
		slog.Debug("station icon source failed", "station", station.Name, "source", "homepage", "url", homepage, "error", err)
		return favicon.DownloadResult{}, err
	}
	result, err := favicon.Download(ctx, parsed, opts)
	if err != nil {
		err = fmt.Errorf("homepage %q: %w", homepage, err)
		slog.Debug("station icon source failed", "station", station.Name, "source", "homepage", "url", homepage, "error", err)
		return favicon.DownloadResult{}, err
	}
	slog.Debug("station icon source selected", "station", station.Name, "source", "homepage", "homepage", homepage, "url", result.URL)
	return result, nil
}

// encodeStationIcon preserves SVG sources or encodes the bounded raster as PNG.
func encodeStationIcon(icon favicon.Icon) ([]byte, error) {
	if len(icon.SVG) > 0 {
		return icon.SVG, nil
	}
	var data bytes.Buffer
	if err := png.Encode(&data, icon.Raster); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

// stationIconKey identifies a station icon source independent of row reuse.
func stationIconKey(station radio.Station) string {
	faviconURL := strings.TrimSpace(station.Favicon)
	homepageURL := strings.TrimSpace(station.Homepage)
	if faviconURL == "" && homepageURL == "" {
		return ""
	}
	return homepageURL + "\x00" + faviconURL
}

// resizeStationIcon preserves rasters up to 256 px and bounds larger ones proportionally.
func resizeStationIcon(source image.Image) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= stationIconSize && height <= stationIconSize {
		return source
	}
	if width >= height {
		height = max(1, height*stationIconSize/width)
		width = stationIconSize
	} else {
		width = max(1, width*stationIconSize/height)
		height = stationIconSize
	}
	resized := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.ApproxBiLinear.Scale(resized, resized.Bounds(), source, bounds, xdraw.Src, nil)
	return resized
}
