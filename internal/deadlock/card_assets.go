package deadlock

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxCardAssetBytes   int64 = 2 << 20
	cardAssetTTL              = time.Hour
	cardAssetFailureTTL       = 5 * time.Minute
)

type cardAssetEntry struct {
	dataURI   string
	expiresAt time.Time
}

// CardAssetLoader downloads small card-safe image assets and converts them to
// embedded SVG data URIs so ImageMagick never resolves external resources.
type CardAssetLoader struct {
	client *http.Client
	now    func() time.Time

	mu      sync.RWMutex
	entries map[string]cardAssetEntry
}

func NewCardAssetLoader(client *http.Client) *CardAssetLoader {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &CardAssetLoader{
		client:  client,
		now:     time.Now,
		entries: make(map[string]cardAssetEntry),
	}
}

func (l *CardAssetLoader) DataURI(ctx context.Context, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if l == nil || rawURL == "" || !safeCardAssetURL(rawURL) {
		return ""
	}

	now := l.now()
	l.mu.RLock()
	cached, ok := l.entries[rawURL]
	l.mu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.dataURI
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		l.cache(rawURL, "", cardAssetFailureTTL)
		return ""
	}
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp")

	response, err := l.client.Do(req)
	if err != nil {
		l.cache(rawURL, "", cardAssetFailureTTL)
		return ""
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices ||
		response.ContentLength > maxCardAssetBytes {
		l.cache(rawURL, "", cardAssetFailureTTL)
		return ""
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, maxCardAssetBytes+1))
	if err != nil || int64(len(payload)) > maxCardAssetBytes {
		l.cache(rawURL, "", cardAssetFailureTTL)
		return ""
	}

	mediaType := cardAssetMediaType(response.Header.Get("Content-Type"), payload)
	if mediaType == "" {
		l.cache(rawURL, "", cardAssetFailureTTL)
		return ""
	}

	dataURI := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(payload)
	l.cache(rawURL, dataURI, cardAssetTTL)
	return dataURI
}

func (l *CardAssetLoader) cache(rawURL string, dataURI string, ttl time.Duration) {
	l.mu.Lock()
	l.entries[rawURL] = cardAssetEntry{dataURI: dataURI, expiresAt: l.now().Add(ttl)}
	l.mu.Unlock()
}

func safeCardAssetURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func cardAssetMediaType(header string, payload []byte) string {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(header, ";")[0]))
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp":
		return mediaType
	}

	switch {
	case len(payload) >= 8 && string(payload[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(payload) >= 3 && payload[0] == 0xff && payload[1] == 0xd8 && payload[2] == 0xff:
		return "image/jpeg"
	case len(payload) >= 12 && string(payload[:4]) == "RIFF" && string(payload[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}
