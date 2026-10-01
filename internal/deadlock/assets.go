package deadlock

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const staticAssetCacheTTL = time.Hour

// Raw shapes from /v1/assets/{heroes,items,ranks}; unused fields are ignored.
type heroAsset struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Images struct {
		Small string `json:"icon_image_small"`
		Card  string `json:"icon_hero_card"`
	} `json:"images"`
}

type itemAsset struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
}

type rankAsset struct {
	Tier   int32             `json:"tier"`
	Name   string            `json:"name"`
	Images map[string]string `json:"images"`
}

// AssetSnapshot contains the static assets this bot uses for richer Discord UI.
type AssetSnapshot struct {
	Heroes map[int64]AssetDetails
	Items  map[int64]AssetDetails
	Ranks  map[int32]rankAsset
}

// AssetCache keeps the static asset API from being hit on every command click.
type AssetCache struct {
	client *Client

	mu        sync.Mutex
	expiresAt time.Time
	snapshot  *AssetSnapshot
}

func NewAssetCache(client *Client) *AssetCache {
	return &AssetCache{client: client}
}

func (c *AssetCache) Snapshot(ctx context.Context) (*AssetSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.snapshot != nil && time.Now().Before(c.expiresAt) {
		return c.snapshot, nil
	}

	snapshot := &AssetSnapshot{
		Heroes: make(map[int64]AssetDetails),
		Items:  make(map[int64]AssetDetails),
		Ranks:  make(map[int32]rankAsset),
	}
	ttl := staticAssetCacheTTL

	// A failed fetch is retried after a minute instead of leaving blank
	// hero/item/rank names for the full hour
	if heroes, err := c.client.HeroAssets(ctx); err == nil {
		for _, h := range heroes {
			snapshot.Heroes[h.ID] = AssetDetails{ID: h.ID, Name: h.Name, IconURL: h.Images.Small, ImageURL: h.Images.Card}
		}
	} else {
		ttl = time.Minute
	}
	if items, err := c.client.ItemAssets(ctx); err == nil {
		for _, item := range items {
			snapshot.Items[item.ID] = AssetDetails{ID: item.ID, Name: item.Name, IconURL: item.Image}
		}
	} else {
		ttl = time.Minute
	}
	if ranks, err := c.client.RankAssets(ctx); err == nil {
		for _, rank := range ranks {
			snapshot.Ranks[rank.Tier] = rank
		}
	} else {
		ttl = time.Minute
	}

	c.snapshot = snapshot
	c.expiresAt = time.Now().Add(ttl)

	return snapshot, nil
}

func (s *AssetSnapshot) Hero(id int32) AssetDetails {
	if asset, ok := s.Heroes[int64(id)]; ok {
		return asset
	}
	return AssetDetails{ID: int64(id), Name: fmt.Sprintf("Hero %d", id)}
}

func (s *AssetSnapshot) Item(id int64) AssetDetails {
	if asset, ok := s.Items[id]; ok {
		return asset
	}
	return AssetDetails{ID: id, Name: fmt.Sprintf("Item %d", id)}
}

// Rank resolves a badge such as 54 (tier 5, sub-rank 4) to the tier name and
// the matching sub-rank badge image.
func (s *AssetSnapshot) Rank(badge int32) AssetDetails {
	rank, ok := s.Ranks[badge/10]
	if !ok {
		return AssetDetails{ID: int64(badge)}
	}
	image := rank.Images[fmt.Sprintf("large_subrank%d", badge%10)]
	if image == "" {
		image = rank.Images["large"]
	}
	return AssetDetails{ID: int64(badge), Name: rank.Name, IconURL: image}
}
