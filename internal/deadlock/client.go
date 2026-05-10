package deadlock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to Deadlock API over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Deadlock API client.
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 12 * time.Second,
		}
	}

	return &Client{
		baseURL:    "https://api.deadlock-api.com",
		httpClient: httpClient,
	}
}

// SearchSteam searches for Steam profiles by account/persona name.
func (c *Client) SearchSteam(ctx context.Context, query string) ([]SteamProfile, error) {
	values := url.Values{}
	values.Set("search_query", query)

	var profiles []SteamProfile
	err := c.getJSON(ctx, "/v1/players/steam-search", values, &profiles)
	if err != nil {
		return nil, err
	}

	return profiles, nil
}

// MatchHistory fetches the player's Deadlock match history.
func (c *Client) MatchHistory(ctx context.Context, accountID int64) ([]MatchHistoryEntry, error) {
	path := fmt.Sprintf("/v1/players/%d/match-history", accountID)

	var history []MatchHistoryEntry
	err := c.getJSON(ctx, path, nil, &history)
	if err != nil {
		return nil, err
	}

	return history, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	fullURL := strings.TrimRight(c.baseURL, "/") + path

	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return fmt.Errorf("create Deadlock API request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "discord-gobot/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call Deadlock API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("Deadlock API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Deadlock API response: %w", err)
	}

	return nil
}
