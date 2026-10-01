package alerts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscribeHandsOverOnlyMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tk_test" || r.URL.Query().Get("since") != "123" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"id":"a","event":"open"}
{"id":"b","event":"keepalive"}
{"id":"c","event":"message","title":"🚨 navidrome failed","message":"logs here","click":"https://example.com"}
`))
	}))
	defer server.Close()

	var got []message
	subscribe(context.Background(), server.URL+"/homelab/json?since=123", "tk_test", func(m message) { got = append(got, m) })

	if len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("subscribe handed over %+v, want only message c", got)
	}
	if text := format(got[0]); text != "**🚨 navidrome failed**\nlogs here\nhttps://example.com" {
		t.Fatalf("format() = %q", text)
	}
	if long := format(message{Message: strings.Repeat("x", 3000)}); len([]rune(long)) != 2000 {
		t.Fatalf("format() of a long alert is %d characters, want 2000", len([]rune(long)))
	}
}
