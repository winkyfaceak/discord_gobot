package serverstats

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestBuildCardSVGShowsLiveScoreboardAndEscapesNames(t *testing.T) {
	snapshot := &Snapshot{
		Name:            `<script>& "server"`,
		Game:            "Counter-Strike",
		Map:             "de_dust2",
		CurrentPlayers:  1,
		MaxPlayers:      24,
		Latency:         42 * time.Millisecond,
		RosterAvailable: true,
		Roster:          []Player{{Name: "<player>&", Score: 17, Connected: 91 * time.Second}},
	}
	view := CardView{
		Endpoint:    Endpoint{Address: "203.0.113.10:27015"},
		Snapshot:    snapshot,
		Status:      StatusLive,
		LastAttempt: time.Date(2026, 5, 27, 12, 10, 0, 0, time.UTC),
		ExpiresAt:   time.Date(2026, 5, 27, 12, 25, 0, 0, time.UTC),
	}

	svg := BuildCardSVG(view)
	for _, want := range []string{"VALVE SERVER MONITOR", "LIVE", "de_dust2", "&lt;script&gt;&amp;", "&lt;player&gt;&amp;", "01m 31s"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("BuildCardSVG() does not contain %q", want)
		}
	}
	if strings.Contains(svg, "<script>") {
		t.Fatal("BuildCardSVG() did not escape untrusted server content")
	}
}

func TestBuildCardSVGRepresentsUnavailableStaleAndTerminalStates(t *testing.T) {
	snapshot := &Snapshot{Name: "Server", RosterAvailable: false, FetchedAt: time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)}
	stale := BuildCardSVG(CardView{Snapshot: snapshot, Status: StatusStale})
	if !strings.Contains(stale, "STALE") || !strings.Contains(stale, "ROSTER UNAVAILABLE") || !strings.Contains(stale, "LAST GOOD 11:00:00 UTC - RETRYING") {
		t.Fatal("stale/unavailable card is missing expected state text")
	}

	offline := BuildCardSVG(CardView{Status: StatusOffline, Detail: "query timed out"})
	if !strings.Contains(offline, "OFFLINE") || !strings.Contains(offline, "Retrying automatically. query timed out") {
		t.Fatal("offline card is missing expected retry state")
	}

	closed := BuildCardSVG(CardView{Snapshot: snapshot, Status: StatusClosed})
	if !strings.Contains(closed, "SESSION CLOSED") {
		t.Fatal("closed card is missing terminal overlay")
	}
}

func TestRenderCardPNGWhenImageMagickIsAvailable(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick is not installed in this test environment")
	}

	image, err := RenderCardPNG(context.Background(), CardView{
		Endpoint: Endpoint{Address: "203.0.113.10:27015"},
		Snapshot: &Snapshot{
			Name:            "Validation Server",
			Game:            "Source",
			Map:             "test_map",
			CurrentPlayers:  1,
			MaxPlayers:      24,
			RosterAvailable: true,
			Roster:          []Player{{Name: "Player", Score: 5}},
		},
		Status: StatusLive,
	})
	if err != nil {
		t.Fatalf("RenderCardPNG() error = %v", err)
	}
	if !bytes.HasPrefix(image, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("RenderCardPNG() did not return PNG bytes")
	}
}
