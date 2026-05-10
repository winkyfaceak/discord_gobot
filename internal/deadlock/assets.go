package deadlock

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const staticAssetCacheTTL = time.Hour

// AssetSnapshot contains the static assets this bot uses for richer Discord UI.
type AssetSnapshot struct {
	Heroes map[int32]AssetDetails
	Items  map[int32]AssetDetails
	Ranks  map[int32]AssetDetails
}

// AssetCache keeps the static asset API from being hit on every command click.
type AssetCache struct {
	client *Client

	mu        sync.RWMutex
	expiresAt time.Time
	snapshot  *AssetSnapshot
}

func NewAssetCache(client *Client) *AssetCache {
	return &AssetCache{client: client}
}

func (c *AssetCache) Snapshot(ctx context.Context) (*AssetSnapshot, error) {
	if c == nil || c.client == nil {
		return emptyAssetSnapshot(), nil
	}

	now := time.Now()

	c.mu.RLock()
	if c.snapshot != nil && now.Before(c.expiresAt) {
		snapshot := c.snapshot
		c.mu.RUnlock()
		return snapshot, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.snapshot != nil && now.Before(c.expiresAt) {
		return c.snapshot, nil
	}

	snapshot := emptyAssetSnapshot()

	if payload, err := c.client.HeroAssets(ctx); err == nil {
		snapshot.Heroes = parseAssetMap(payload, "heroes")
	}
	if payload, err := c.client.ItemAssets(ctx); err == nil {
		snapshot.Items = parseAssetMap(payload, "items")
	}
	if payload, err := c.client.RankAssets(ctx); err == nil {
		snapshot.Ranks = parseAssetMap(payload, "ranks")
	}

	c.snapshot = snapshot
	c.expiresAt = time.Now().Add(staticAssetCacheTTL)

	return snapshot, nil
}

func emptyAssetSnapshot() *AssetSnapshot {
	return &AssetSnapshot{
		Heroes: make(map[int32]AssetDetails),
		Items:  make(map[int32]AssetDetails),
		Ranks:  make(map[int32]AssetDetails),
	}
}

func (s *AssetSnapshot) Hero(id int32) AssetDetails {
	if s == nil {
		return AssetDetails{ID: id, Name: fmt.Sprintf("Hero %d", id)}
	}
	if asset, ok := s.Heroes[id]; ok {
		return asset
	}
	return AssetDetails{ID: id, Name: fmt.Sprintf("Hero %d", id)}
}

func (s *AssetSnapshot) Item(id int32) AssetDetails {
	if s == nil {
		return AssetDetails{ID: id, Name: fmt.Sprintf("Item %d", id)}
	}
	if asset, ok := s.Items[id]; ok {
		return asset
	}
	return AssetDetails{ID: id, Name: fmt.Sprintf("Item %d", id)}
}

func (s *AssetSnapshot) Rank(badge int32) AssetDetails {
	if s == nil {
		return AssetDetails{ID: badge}
	}
	if asset, ok := s.Ranks[badge]; ok {
		return asset
	}
	if badge > 0 {
		if asset, ok := s.Ranks[badge/10]; ok {
			return asset
		}
	}
	return AssetDetails{ID: badge}
}

func parseAssetMap(payload any, kind string) map[int32]AssetDetails {
	result := make(map[int32]AssetDetails)
	walkAssetPayload(payload, "", result)

	// Ranks sometimes describe only a tier, while player badges include subtier
	// digits. Mirror tier assets across their likely badge slots so lookups work.
	if kind == "ranks" {
		for id, asset := range result {
			if id > 0 && id < 10 {
				for sub := int32(1); sub <= 6; sub++ {
					badge := id*10 + sub
					if _, exists := result[badge]; !exists {
						copy := asset
						copy.ID = badge
						result[badge] = copy
					}
				}
			}
		}
	}

	return result
}

func walkAssetPayload(value any, keyHint string, result map[int32]AssetDetails) {
	switch v := value.(type) {
	case []any:
		for _, child := range v {
			walkAssetPayload(child, "", result)
		}
	case map[string]any:
		if asset, ok := assetFromObject(v, keyHint); ok {
			mergeAsset(result, asset)
		}

		for key, child := range v {
			walkAssetPayload(child, key, result)
		}
	}
}

func assetFromObject(object map[string]any, keyHint string) (AssetDetails, bool) {
	id, hasID := numericObjectField(object,
		"id",
		"hero_id",
		"item_id",
		"rank",
		"tier",
		"badge",
		"badge_id",
		"rank_id",
	)
	if !hasID {
		if parsed, err := strconv.ParseInt(keyHint, 10, 32); err == nil {
			id = int32(parsed)
			hasID = true
		}
	}
	if !hasID {
		return AssetDetails{}, false
	}

	asset := AssetDetails{
		ID:       id,
		Name:     bestStringObjectField(object, "name", "display_name", "localized_name", "hero_name", "item_name", "title", "rank_name"),
		IconURL:  normalizeAssetURL(bestImageURLInValue(object, true)),
		ImageURL: normalizeAssetURL(bestImageURLInValue(object, false)),
	}
	if asset.IconURL == "" {
		asset.IconURL = asset.ImageURL
	}

	return asset, asset.Name != "" || asset.IconURL != "" || asset.ImageURL != ""
}

func mergeAsset(result map[int32]AssetDetails, incoming AssetDetails) {
	current, exists := result[incoming.ID]
	if !exists {
		result[incoming.ID] = incoming
		return
	}
	if current.Name == "" && incoming.Name != "" {
		current.Name = incoming.Name
	}
	if current.IconURL == "" && incoming.IconURL != "" {
		current.IconURL = incoming.IconURL
	}
	if current.ImageURL == "" && incoming.ImageURL != "" {
		current.ImageURL = incoming.ImageURL
	}
	result[incoming.ID] = current
}

func numericObjectField(object map[string]any, keys ...string) (int32, bool) {
	for _, key := range keys {
		value, ok := object[key]
		if !ok {
			continue
		}
		if parsed, ok := valueToInt32(value); ok {
			return parsed, true
		}
	}
	return 0, false
}

func valueToInt32(value any) (int32, bool) {
	switch v := value.(type) {
	case float64:
		return int32(v), true
	case int:
		return int32(v), true
	case int32:
		return v, true
	case int64:
		return int32(v), true
	case string:
		parsed, err := strconv.ParseInt(v, 10, 32)
		if err == nil {
			return int32(parsed), true
		}
	}
	return 0, false
}

func bestStringObjectField(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	for _, value := range object {
		child, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if found := bestStringObjectField(child, keys...); found != "" {
			return found
		}
	}

	return ""
}

func bestImageURLInValue(value any, preferIcon bool) string {
	var fallback string

	var walk func(any)
	walk = func(current any) {
		if fallback != "" {
			return
		}

		switch v := current.(type) {
		case string:
			lower := strings.ToLower(v)
			if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(v, "/") {
				if looksLikeImageURL(lower) {
					fallback = v
				}
			}
		case []any:
			for _, child := range v {
				walk(child)
				if fallback != "" {
					return
				}
			}
		case map[string]any:
			keys := []string{"icon", "icon_url", "icon_webp", "small_image", "image", "image_url", "large_image", "portrait", "thumbnail"}
			if !preferIcon {
				keys = []string{"image", "image_url", "large_image", "portrait", "thumbnail", "icon", "icon_url", "icon_webp", "small_image"}
			}

			for _, key := range keys {
				if child, ok := v[key]; ok {
					walk(child)
					if fallback != "" {
						return
					}
				}
			}

			for _, child := range v {
				walk(child)
				if fallback != "" {
					return
				}
			}
		}
	}

	walk(value)
	return fallback
}

func looksLikeImageURL(value string) bool {
	return strings.Contains(value, ".png") ||
		strings.Contains(value, ".jpg") ||
		strings.Contains(value, ".jpeg") ||
		strings.Contains(value, ".webp") ||
		strings.Contains(value, ".gif") ||
		strings.Contains(value, "image") ||
		strings.Contains(value, "icon")
}

func normalizeAssetURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return "https://assets.deadlock-api.com" + raw
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		return raw
	}
	return "https://assets.deadlock-api.com/" + strings.TrimLeft(raw, "/")
}
