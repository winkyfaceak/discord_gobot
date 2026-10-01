package deadlock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
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

// SteamProfiles fetches Steam profile details for a batch of SteamID3 account IDs.
func (c *Client) SteamProfiles(ctx context.Context, accountIDs []int64) ([]SteamProfile, error) {
	if len(accountIDs) == 0 {
		return nil, nil
	}

	parts := make([]string, 0, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		parts = append(parts, strconv.FormatInt(accountID, 10))
	}

	if len(parts) == 0 {
		return nil, nil
	}

	values := url.Values{}
	values.Set("account_ids", strings.Join(parts, ","))

	var profiles []SteamProfile
	err := c.getJSON(ctx, "/v1/players/steam", values, &profiles)
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

// RankPrediction fetches the Deadlock API rank prediction for a player.
func (c *Client) RankPrediction(ctx context.Context, accountID int64) (*RankPrediction, error) {
	path := fmt.Sprintf("/v1/players/%d/rank-predict", accountID)

	var prediction RankPrediction
	err := c.getJSON(ctx, path, nil, &prediction)
	if err != nil {
		return nil, err
	}

	return &prediction, nil
}

// ActiveMatches fetches active matches and filters them by account ID.
func (c *Client) ActiveMatches(ctx context.Context, accountID int64) ([]ActiveMatch, error) {
	values := url.Values{}
	values.Set("account_ids", strconv.FormatInt(accountID, 10))

	var matches []ActiveMatch
	err := c.getJSON(ctx, "/v1/matches/active", values, &matches)
	if err != nil {
		return nil, err
	}

	return matches, nil
}

// HeroBuildStats fetches build performance stats for a hero.
func (c *Client) HeroBuildStats(ctx context.Context, heroID int32, accountID int64) ([]HeroBuildStats, error) {
	path := fmt.Sprintf("/v1/analytics/hero-build-stats/%d", heroID)
	values := url.Values{}
	values.Set("min_matches", "1")
	if accountID > 0 {
		values.Set("account_ids", strconv.FormatInt(accountID, 10))
	}

	var stats []HeroBuildStats
	err := c.getJSON(ctx, path, values, &stats)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// BuildItemStats fetches popular item usage from hero builds.
func (c *Client) BuildItemStats(ctx context.Context, heroID int32) ([]BuildItemStats, error) {
	values := url.Values{}
	if heroID > 0 {
		values.Set("hero_id", strconv.FormatInt(int64(heroID), 10))
	}

	var stats []BuildItemStats
	err := c.getJSON(ctx, "/v1/analytics/build-item-stats", values, &stats)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

// HeroAssets fetches hero names and icons.
func (c *Client) HeroAssets(ctx context.Context) ([]heroAsset, error) {
	var heroes []heroAsset
	return heroes, c.getJSON(ctx, "/v1/assets/heroes", nil, &heroes)
}

// ItemAssets fetches item names and icons.
func (c *Client) ItemAssets(ctx context.Context) ([]itemAsset, error) {
	var items []itemAsset
	return items, c.getJSON(ctx, "/v1/assets/items", nil, &items)
}

// RankAssets fetches rank names and badge images.
func (c *Client) RankAssets(ctx context.Context) ([]rankAsset, error) {
	var ranks []rankAsset
	return ranks, c.getJSON(ctx, "/v1/assets/ranks", nil, &ranks)
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, target any) error {
	fullURL := c.baseURL + path

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
		return fmt.Errorf("deadlock API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode Deadlock API response: %w", err)
	}

	return nil
}
