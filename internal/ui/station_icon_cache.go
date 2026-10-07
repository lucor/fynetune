package ui

import "github.com/hashicorp/golang-lru/v2"

// stationIconCacheCapacity bounds the number of decoded icons retained in RAM.
// A 256x256 NRGBA raster uses 256 KiB of pixel data, so 64 maximum-size rasters
// use about 16 MiB, plus object overhead. SVG resources retain their source
// bytes instead, so this entry-count limit is not a strict byte limit.
const stationIconCacheCapacity = 64

// stationIconCache is the in-memory tier of station artwork caching. It holds
// decoded, display-sized icons for fast station and section switches. The
// encoded icon is also stored by cacheStore in Fyne's app cache for 30 days;
// after an LRU eviction a fresh disk-cache hit needs a local read and decode,
// without downloading the icon again. Expired cached data is shown while a
// background refresh runs.
type stationIconCache struct {
	entries *lru.Cache[string, stationIcon]
}

func newStationIconCache() *stationIconCache {
	entries, err := lru.New[string, stationIcon](stationIconCacheCapacity)
	if err != nil {
		panic("station icon cache capacity must be positive: " + err.Error())
	}
	return &stationIconCache{entries: entries}
}

// get returns an icon and marks it recently used for LRU eviction.
func (c *stationIconCache) get(key string) (stationIcon, bool) {
	return c.entries.Get(key)
}

// add stores an icon, evicting the least recently used item at capacity.
func (c *stationIconCache) add(key string, icon stationIcon) {
	c.entries.Add(key, icon)
}

func (w *Window) cacheStationIcon(key string, icon stationIcon) {
	if w.faviconCache == nil {
		w.faviconCache = newStationIconCache()
	}
	w.faviconCache.add(key, icon)
}
