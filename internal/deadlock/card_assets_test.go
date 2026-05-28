package deadlock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var tinyCardPNG = []byte{
	0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
	0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
}

func TestCardAssetLoaderLoadsHTTPSImagesAndCachesResult(t *testing.T) {
	var requests int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyCardPNG)
	}))
	defer server.Close()

	loader := NewCardAssetLoader(server.Client())
	first := loader.DataURI(context.Background(), server.URL+"/rank.png")
	second := loader.DataURI(context.Background(), server.URL+"/rank.png")

	if !strings.HasPrefix(first, "data:image/png;base64,") || second != first {
		t.Fatalf("DataURI() = %q, %q; want matching PNG data URIs", first, second)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("asset was fetched %d times; want cached after first fetch", got)
	}
}

func TestCardAssetLoaderRejectsUnusableResources(t *testing.T) {
	loader := NewCardAssetLoader(&http.Client{Timeout: time.Second})
	if got := loader.DataURI(context.Background(), "http://assets.deadlock-api.com/icon.png"); got != "" {
		t.Fatalf("DataURI() accepted non-HTTPS asset: %q", got)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()
	loader = NewCardAssetLoader(server.Client())
	if got := loader.DataURI(context.Background(), server.URL+"/bad"); got != "" {
		t.Fatalf("DataURI() accepted non-image bytes: %q", got)
	}
}
