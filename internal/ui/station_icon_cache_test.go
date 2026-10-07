package ui

import (
	"strconv"
	"testing"
)

func TestStationIconCacheEvictsLeastRecentlyUsed(t *testing.T) {
	cache := newStationIconCache()
	for i := 0; i < stationIconCacheCapacity; i++ {
		cache.add(strconv.Itoa(i), stationIcon{})
	}

	if _, ok := cache.get("0"); !ok {
		t.Fatal("recently used icon was not found")
	}
	cache.add("new", stationIcon{})

	if _, ok := cache.get("1"); ok {
		t.Fatal("least recently used icon was not evicted")
	}
	if _, ok := cache.get("0"); !ok {
		t.Fatal("recently used icon was evicted")
	}
	if got := cache.entries.Len(); got != stationIconCacheCapacity {
		t.Fatalf("cache size = %d, want %d", got, stationIconCacheCapacity)
	}
}
