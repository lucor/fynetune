// Package radiobrowser provides a small client for the Radio Browser station API.
package radiobrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.lucor.dev/fynetune/internal/radio"
	"go.lucor.dev/fynetune/internal/version"
)

// ErrHTTPStatus indicates that Radio Browser returned an unsuccessful status.
var ErrHTTPStatus = errors.New("radio browser returned unsuccessful HTTP status")

// maxResponseBytes bounds Radio Browser JSON responses before decoding.
const maxResponseBytes = 256 << 10

// defaultBaseURL is the Radio Browser API endpoint used by New.
const defaultBaseURL = "https://all.api.radio-browser.info"

// Query contains the filters and result limit for a station search.
type Query struct {
	Name         string   // Station name filter.
	Country      string   // Country name filter.
	CountryCode  string   // ISO 3166-1 alpha-2 country code filter.
	CountryExact bool     // Match Country exactly instead of searching within it.
	Tag          string   // Tag filter.
	TagExact     bool     // Match Tag exactly instead of searching within it.
	Tags         []string // Alternative tags; any matching tag can include a station.
	Codec        string   // Audio codec filter.
	Limit        int      // Maximum results; values outside 1..100 use the default.
}

// Country is a country name and ISO 3166-1 alpha-2 code from Radio Browser.
type Country struct {
	Name string `json:"name"`
	Code string `json:"iso_3166_1"`
}

// Directory provides station search, popularity, and country data to the UI.
type Directory interface {
	Search(context.Context, Query) ([]radio.Station, error)
	Popular(context.Context, int) ([]radio.Station, error)
	Countries(context.Context) ([]Country, error)
}

// Client queries the Radio Browser station directory.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a Radio Browser client using the default API endpoint.
// A nil HTTP client is replaced with one that has a 15-second timeout.
func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: defaultBaseURL, http: client}
}

// NewWithBaseURL creates a client pointed at a custom API endpoint.
// It is useful for controlled deployments and tests.
func NewWithBaseURL(baseURL string, client *http.Client) *Client {
	c := New(client)
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

// Search finds stations matching the supplied filters and maps valid results to radio stations.
// When Tags contains alternatives, each tag is searched separately and the
// results are interleaved by rank with duplicate stations removed.
func (c *Client) Search(ctx context.Context, query Query) ([]radio.Station, error) {
	if len(query.Tags) == 0 {
		return c.search(ctx, query)
	}

	groups := make([][]radio.Station, 0, len(query.Tags))
	seenTags := make(map[string]struct{}, len(query.Tags))
	for _, tag := range query.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seenTags[key]; exists {
			continue
		}
		seenTags[key] = struct{}{}

		tagQuery := query
		tagQuery.Tag = tag
		tagQuery.TagExact = true
		tagQuery.Tags = nil
		stations, err := c.search(ctx, tagQuery)
		if err != nil {
			return nil, fmt.Errorf("search stations by tag %q: %w", tag, err)
		}
		groups = append(groups, stations)
	}
	if len(groups) == 0 {
		return c.search(ctx, query)
	}
	return mergeStationGroups(groups, resultLimit(query.Limit)), nil
}

// search performs one station search with at most one tag filter.
func (c *Client) search(ctx context.Context, query Query) ([]radio.Station, error) {
	values := url.Values{}
	if name := strings.TrimSpace(query.Name); name != "" {
		values.Set("name", name)
	}
	country := strings.TrimSpace(query.Country)
	if country != "" {
		values.Set("country", country)
	}
	if countryCode := strings.TrimSpace(query.CountryCode); countryCode != "" {
		values.Set("countrycode", countryCode)
	}
	if query.CountryExact && country != "" {
		values.Set("countryExact", "true")
	}
	if tag := strings.TrimSpace(query.Tag); tag != "" {
		values.Set("tag", tag)
		if query.TagExact {
			values.Set("tagExact", "true")
		}
	}
	if codec := strings.TrimSpace(query.Codec); codec != "" {
		values.Set("codec", codec)
	}
	values.Set("hidebroken", "true")
	values.Set("order", "clickcount")
	values.Set("reverse", "true")
	setLimit(values, query.Limit)
	return c.fetch(ctx, "/json/stations/search", values)
}

// mergeStationGroups combines tag alternatives in rank order and removes duplicate stations.
func mergeStationGroups(groups [][]radio.Station, limit int) []radio.Station {
	stations := make([]radio.Station, 0, limit)
	seen := make(map[string]struct{})
	for rank := 0; len(stations) < limit; rank++ {
		foundAtRank := false
		for _, group := range groups {
			if rank >= len(group) {
				continue
			}
			foundAtRank = true
			station := group[rank]
			if _, exists := seen[station.ID]; exists {
				continue
			}
			seen[station.ID] = struct{}{}
			stations = append(stations, station)
			if len(stations) == limit {
				break
			}
		}
		if !foundAtRank {
			break
		}
	}
	return stations
}

// Popular returns the most-clicked stations up to the requested limit.
func (c *Client) Popular(ctx context.Context, limit int) ([]radio.Station, error) {
	values := url.Values{"hidebroken": {"true"}}
	setLimit(values, limit)
	return c.fetch(ctx, "/json/stations/topclick", values)
}

// Countries returns countries with non-empty names and normalized country codes.
func (c *Client) Countries(ctx context.Context) ([]Country, error) {
	var records []Country
	if err := c.getJSON(ctx, "/json/countries", nil, &records); err != nil {
		return nil, err
	}
	countries := make([]Country, 0, len(records))
	for _, record := range records {
		record.Name = strings.TrimSpace(record.Name)
		record.Code = strings.ToUpper(strings.TrimSpace(record.Code))
		if record.Name != "" {
			countries = append(countries, record)
		}
	}
	return countries, nil
}

// fetch decodes station records and omits entries missing required station fields.
func (c *Client) fetch(ctx context.Context, path string, query url.Values) ([]radio.Station, error) {
	var records []stationRecord
	if err := c.getJSON(ctx, path, query, &records); err != nil {
		return nil, err
	}
	stations := make([]radio.Station, 0, len(records))
	for _, record := range records {
		station := record.station()
		if station.ID != "" && station.Name != "" && station.URL != "" {
			stations = append(stations, station)
		}
	}
	return stations, nil
}

// getJSON fetches one API endpoint and decodes its bounded JSON response.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	endpoint := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create radio browser request: %w", err)
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("radio browser request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%w: %d", ErrHTTPStatus, resp.StatusCode)
	}
	// Read one byte beyond the limit so oversized responses are rejected, not truncated.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read radio browser response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("radio browser response exceeds %d-byte limit", maxResponseBytes)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode radio browser response: %w", err)
	}
	return nil
}

// setLimit applies the API's supported result range and default limit.
func setLimit(values url.Values, limit int) {
	values.Set("limit", strconv.Itoa(resultLimit(limit)))
}

// resultLimit applies Radio Browser's supported range and default.
func resultLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 30
	}
	return limit
}

// stationRecord mirrors the fields consumed from a Radio Browser station response.
type stationRecord struct {
	UUID        string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	ResolvedURL string `json:"url_resolved"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	Country     string `json:"country"`
	CountryCode string `json:"countrycode"`
	Language    string `json:"language"`
	Tags        string `json:"tags"`
}

// station converts the API record into the app's normalized station model.
func (s stationRecord) station() radio.Station {
	var tags []string
	for _, tag := range strings.Split(s.Tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return radio.Station{
		ID: s.UUID, Source: radio.StationSourceRadioBrowser, Name: strings.TrimSpace(s.Name), URL: strings.TrimSpace(s.URL),
		ResolvedURL: strings.TrimSpace(s.ResolvedURL), Homepage: strings.TrimSpace(s.Homepage),
		Favicon: strings.TrimSpace(s.Favicon), Codec: strings.TrimSpace(s.Codec),
		Bitrate: s.Bitrate, Country: strings.TrimSpace(s.Country),
		CountryCode: strings.ToUpper(strings.TrimSpace(s.CountryCode)),
		Language:    strings.TrimSpace(s.Language), Tags: tags,
	}
}
