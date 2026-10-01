package commands

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"discord_gobot/internal/navidrome"
)

// fakeOgg is an Ogg Opus stream: two header packets, then one-byte packets.
func fakeOgg(packets ...string) []byte {
	segments := []byte{8, 4}
	data := []byte("OpusHeadTags")
	for _, p := range packets {
		segments = append(segments, byte(len(p)))
		data = append(data, p...)
	}
	page := append([]byte("OggS"), make([]byte, 22)...)
	page = append(page, byte(len(segments)))
	page = append(page, segments...)
	return append(page, data...)
}

func TestMusicPlayerPlaysQueueInOrderSkipsAndGoesIdle(t *testing.T) {
	streams := map[string][]byte{"a": fakeOgg("1", "2"), "b": fakeOgg("3")}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		w.Write(streams[r.URL.Query().Get("id")])
	}))
	defer server.Close()

	p := &musicPlayer{nav: navidrome.New(server.URL, "user", "pass"), onIdle: func() {}}
	frame := func() string {
		packet, err := p.ProvideOpusFrame()
		if err != nil {
			t.Fatalf("ProvideOpusFrame() error = %v", err)
		}
		return string(packet)
	}

	if position := p.enqueue([]navidrome.Song{{ID: "a"}, {ID: "b"}}, 0); position != 0 {
		t.Fatalf("first enqueue position = %d, want 0", position)
	}
	if got := frame(); got != "1" {
		t.Fatalf("first frame = %q, want song a", got)
	}
	if position := p.enqueue([]navidrome.Song{{ID: "a"}}, 0); position != 2 {
		t.Fatalf("enqueue while playing position = %d, want 2", position)
	}
	if song, ok := p.skip(); !ok || song.ID != "a" {
		t.Fatalf("skip() = %v, %v, want song a", song, ok)
	}
	for _, want := range []string{"3", "1", "2", ""} {
		if got := frame(); got != want {
			t.Fatalf("frame = %q, want %q", got, want)
		}
	}
	if p.idle == nil || p.current != nil {
		t.Fatal("empty queue did not start the idle timer")
	}
	p.idle.Stop()
}
