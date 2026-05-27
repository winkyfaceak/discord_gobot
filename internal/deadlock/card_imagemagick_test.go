package deadlock

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"os/exec"
	"strings"
	"testing"
)

const testCardDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestBuildDashboardSVGIncludesAllViewContentAndEscapesText(t *testing.T) {
	summary := dashboardTestSummary()
	tests := []struct {
		view CardView
		want string
	}{
		{CardViewOverview, "SIGNATURE HERO"},
		{CardViewRank, "PREDICTED RANK"},
		{CardViewRecent, "RECENT MATCH LEDGER"},
		{CardViewCurrent, "ACTIVE MATCH WATCH"},
		{CardViewBuilds, "BUILDS AND ITEMS"},
		{CardViewAll, "FULL SNAPSHOT"},
	}

	for _, test := range tests {
		t.Run(string(test.view), func(t *testing.T) {
			svg := BuildDashboardSVG(summary, CardOptions{View: test.view})
			if !strings.Contains(svg, test.want) {
				t.Fatalf("BuildDashboardSVG(%q) does not contain %q", test.view, test.want)
			}
			if !strings.Contains(svg, "Player &lt;One&gt;") {
				t.Fatal("BuildDashboardSVG() did not escape the player name")
			}
			if strings.Contains(svg, "Player <One>") {
				t.Fatal("BuildDashboardSVG() includes raw untrusted text")
			}
		})
	}
}

func TestBuildDashboardSVGEmbedsAssetsForEveryVisualPage(t *testing.T) {
	summary := dashboardTestSummaryWithImages()
	images := dashboardCardImages{byURL: map[string]string{
		summary.TopHeroIconURL:                 testCardDataURI,
		summary.Rank.ImageURL:                  testCardDataURI,
		summary.RecentMatches[0].HeroIconURL:   testCardDataURI,
		summary.CurrentGame.Player.HeroIconURL: testCardDataURI,
		summary.Build.HeroIconURL:              testCardDataURI,
		summary.Build.PopularItems[0].IconURL:  testCardDataURI,
	}}
	for _, view := range []CardView{CardViewOverview, CardViewRank, CardViewRecent, CardViewCurrent, CardViewBuilds, CardViewAll} {
		t.Run(string(view), func(t *testing.T) {
			svg := buildDashboardSVG(summary, CardOptions{View: view, UseRankImage: true}, images)
			if !strings.Contains(svg, `<image xlink:href="data:image/png;base64,`) {
				t.Fatalf("buildDashboardSVG(%q) did not place available artwork", view)
			}
		})
	}

	svg := buildDashboardSVG(summary, CardOptions{View: CardViewRank, UseRankImage: false}, images)
	if strings.Contains(svg, `<image xlink:href="data:image/png;base64,`) {
		t.Fatal("rank artwork was rendered while rank-image was disabled")
	}
}

func TestDashboardCardAssetURLsSelectOnlyVisibleArtwork(t *testing.T) {
	summary := dashboardTestSummaryWithImages()
	if got := dashboardCardAssetURLs(summary, CardOptions{View: CardViewRank, UseRankImage: false}); len(got) != 0 {
		t.Fatalf("rank-image false selected assets %v; want none", got)
	}
	if got := dashboardCardAssetURLs(summary, CardOptions{View: CardViewBuilds}); len(got) != 2 {
		t.Fatalf("builds selected %d assets; want hero and item artwork", len(got))
	}
	if got := dashboardCardAssetURLs(summary, CardOptions{View: CardViewOverview}); len(got) != 3 {
		t.Fatalf("overview selected %d assets; want top hero and two recent heroes", len(got))
	}
}

func TestRenderDashboardPNGViewsWhenImageMagickIsAvailable(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick is not installed in this test environment")
	}

	for _, view := range []CardView{CardViewOverview, CardViewRank, CardViewRecent, CardViewCurrent, CardViewBuilds, CardViewAll} {
		t.Run(string(view), func(t *testing.T) {
			png, err := RenderDashboardPNG(context.Background(), dashboardTestSummary(), CardOptions{View: view})
			if err != nil {
				t.Fatalf("RenderDashboardPNG(%q) error = %v", view, err)
			}
			if !bytes.HasPrefix(png, []byte{0x89, 'P', 'N', 'G'}) {
				t.Fatal("RenderDashboardPNG() did not return PNG bytes")
			}
			config, _, err := image.DecodeConfig(bytes.NewReader(png))
			if err != nil {
				t.Fatalf("DecodeConfig() error = %v", err)
			}
			if config.Width != deadlockCardRenderWidth || config.Height != deadlockCardRenderHeight {
				t.Fatalf("PNG dimensions = %dx%d; want %dx%d", config.Width, config.Height, deadlockCardRenderWidth, deadlockCardRenderHeight)
			}
		})
	}

	t.Run("embedded_artwork", func(t *testing.T) {
		image, err := renderDashboardPNG(
			context.Background(),
			dashboardTestSummaryWithImages(),
			CardOptions{View: CardViewBuilds},
			dashboardCardImages{byURL: map[string]string{
				"https://assets.deadlock-api.com/heroes/warden.png":   testCardDataURI,
				"https://assets.deadlock-api.com/items/fleetfoot.png": testCardDataURI,
			}},
		)
		if err != nil {
			t.Fatalf("renderDashboardPNG() with embedded artwork error = %v", err)
		}
		if !bytes.HasPrefix(image, []byte{0x89, 'P', 'N', 'G'}) {
			t.Fatal("renderDashboardPNG() with embedded artwork did not return PNG bytes")
		}
	})
}

func dashboardTestSummaryWithImages() PlayerSummary {
	summary := dashboardTestSummary()
	summary.TopHeroIconURL = "https://assets.deadlock-api.com/heroes/warden.png"
	summary.RecentMatches[0].HeroIconURL = "https://assets.deadlock-api.com/heroes/warden-small.png"
	summary.RecentMatches[1].HeroIconURL = "https://assets.deadlock-api.com/heroes/ivy.png"
	summary.Rank.ImageURL = "https://assets.deadlock-api.com/ranks/oracle.png"
	summary.CurrentGame.Player.HeroIconURL = summary.TopHeroIconURL
	summary.Build.HeroIconURL = summary.TopHeroIconURL
	summary.Build.PopularItems[0].IconURL = "https://assets.deadlock-api.com/items/fleetfoot.png"
	return summary
}

func dashboardTestSummary() PlayerSummary {
	team := int32(0)
	heroID := int32(3)
	duration := int32(741)
	matchID := int64(12345)
	souls := int32(45210)

	return PlayerSummary{
		AccountID:      789,
		Name:           "Player <One>",
		Matches:        88,
		Wins:           51,
		Losses:         37,
		WinRate:        58.0,
		AvgKills:       8.2,
		AvgDeaths:      4.1,
		AvgAssists:     11.7,
		AvgNetWorth:    33190,
		AvgLastHits:    101.2,
		AvgDenies:      7.8,
		TopHeroID:      3,
		TopHeroName:    "Warden",
		TopHeroMatches: 29,
		TopHeroWinRate: 62.1,
		RecentMatches: []RecentMatch{
			{MatchID: 101, HeroID: 3, HeroName: "Warden", Won: true, Kills: 9, Deaths: 3, Assists: 12, NetWorth: 34000, DurationMins: 32},
			{MatchID: 102, HeroID: 5, HeroName: "Ivy", Won: false, Kills: 3, Deaths: 7, Assists: 8, NetWorth: 23000, DurationMins: 27},
		},
		Rank: &RankPrediction{Badge: 74, Name: "Oracle", RawScore: 1721.3, MatchesUsed: 71},
		CurrentGame: &CurrentGameStatus{
			InGame: true,
			Match: &ActiveMatch{
				MatchID:          &matchID,
				DurationS:        &duration,
				NetWorthTeam0:    &souls,
				MatchModeParsed:  "Ranked",
				RegionModeParsed: "Europe",
				Players: []ActiveMatchPlayer{
					{Team: &team, HeroID: &heroID, DisplayName: "Player <One>", HeroName: "Warden"},
				},
			},
			Player: &ActiveMatchPlayer{Team: &team, HeroID: &heroID, DisplayName: "Player <One>", HeroName: "Warden"},
		},
		Build: &BuildInsight{
			HeroID:           3,
			HeroName:         "Warden",
			BuildsSourceNote: "Player sample",
			Builds: []HeroBuildInsight{
				{HeroBuildID: 77, Matches: 20, WinRate: 60.0},
			},
			PopularItems: []ItemBuildInsight{{ItemID: 2, Name: "Fleetfoot", Builds: 51}},
		},
	}
}
