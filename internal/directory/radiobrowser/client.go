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

var ErrHTTPStatus = errors.New("radio browser returned unsuccessful HTTP status")

const defaultBaseURL = "https://all.api.radio-browser.info"

type Query struct {
	Name         string
	Country      string
	CountryCode  string
	CountryExact bool
	Tag          string
	Codec        string
	Limit        int
}

type Country struct {
	Name string `json:"name"`
	Code string `json:"iso_3166_1"`
}

type Directory interface {
	Search(context.Context, Query) ([]radio.Station, error)
	Popular(context.Context, int) ([]radio.Station, error)
	Countries(context.Context) ([]Country, error)
}

type Client struct {
	baseURL string
	http    *http.Client
}

func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: defaultBaseURL, http: client}
}

// NewWithBaseURL is intended for controlled deployments and tests.
func NewWithBaseURL(baseURL string, client *http.Client) *Client {
	c := New(client)
	c.baseURL = strings.TrimRight(baseURL, "/")
	return c
}

func (c *Client) Search(ctx context.Context, query Query) ([]radio.Station, error) {
	values := url.Values{}
	set(values, "name", query.Name)
	set(values, "country", query.Country)
	set(values, "countrycode", query.CountryCode)
	if query.CountryExact && strings.TrimSpace(query.Country) != "" {
		values.Set("countryExact", "true")
	}
	set(values, "tag", query.Tag)
	set(values, "codec", query.Codec)
	values.Set("hidebroken", "true")
	values.Set("order", "clickcount")
	values.Set("reverse", "true")
	setLimit(values, query.Limit)
	return c.fetch(ctx, "/json/stations/search", values)
}

func (c *Client) Popular(ctx context.Context, limit int) ([]radio.Station, error) {
	values := url.Values{"hidebroken": {"true"}}
	setLimit(values, limit)
	return c.fetch(ctx, "/json/stations/topclick", values)
}

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
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 8<<20))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode radio browser response: %w", err)
	}
	return nil
}

func set(values url.Values, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		values.Set(key, value)
	}
}

func setLimit(values url.Values, limit int) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	values.Set("limit", strconv.Itoa(limit))
}

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
	Language    string `json:"language"`
	Tags        string `json:"tags"`
}

func (s stationRecord) station() radio.Station {
	var tags []string
	for _, tag := range strings.Split(s.Tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return radio.Station{
		ID: s.UUID, Name: strings.TrimSpace(s.Name), URL: strings.TrimSpace(s.URL),
		ResolvedURL: strings.TrimSpace(s.ResolvedURL), Homepage: strings.TrimSpace(s.Homepage),
		Favicon: strings.TrimSpace(s.Favicon), Codec: strings.TrimSpace(s.Codec),
		Bitrate: s.Bitrate, Country: strings.TrimSpace(s.Country),
		Language: strings.TrimSpace(s.Language), Tags: tags,
	}
}
