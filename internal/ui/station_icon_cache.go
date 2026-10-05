package ui

// Keep one decoded icon per source. The persistent cache already keeps the
// encoded bytes on disk; this map only avoids decoding the same station again.
type stationIconCache map[string]stationIcon

func newStationIconCache() stationIconCache { return make(stationIconCache) }

func (c stationIconCache) get(key string) (stationIcon, bool) {
	icon, ok := c[key]
	return icon, ok
}

func (w *Window) cacheStationIcon(key string, icon stationIcon) {
	if w.faviconCache == nil {
		w.faviconCache = newStationIconCache()
	}
	w.faviconCache[key] = icon
}
