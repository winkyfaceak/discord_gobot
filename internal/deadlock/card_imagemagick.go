package deadlock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type CardView string

const (
	CardViewOverview CardView = "overview"
	CardViewRank     CardView = "rank"
	CardViewRecent   CardView = "recent"
	CardViewCurrent  CardView = "current"
	CardViewBuilds   CardView = "builds"
	CardViewAll      CardView = "all"

	deadlockCardRenderWidth  = 1680
	deadlockCardRenderHeight = 1080
)

const recentCardPageSize = 5

// CardOptions controls the visible page of the interactive Deadlock card.
type CardOptions struct {
	View         CardView
	Page         int
	UseRankImage bool
}

type dashboardCardImages struct {
	byURL map[string]string
}

var defaultCardAssetLoader = NewCardAssetLoader(nil)

// RenderCardPNG renders the overview card for noninteractive compatibility.
func RenderCardPNG(ctx context.Context, summary PlayerSummary) ([]byte, error) {
	return RenderDashboardPNG(ctx, summary, CardOptions{View: CardViewOverview})
}

// RenderDashboardPNG renders a full interactive Deadlock card using ImageMagick.
func RenderDashboardPNG(ctx context.Context, summary PlayerSummary, options CardOptions) ([]byte, error) {
	return renderDashboardPNG(ctx, summary, options, loadDashboardCardImages(ctx, summary, options, defaultCardAssetLoader))
}

func renderDashboardPNG(ctx context.Context, summary PlayerSummary, options CardOptions, images dashboardCardImages) ([]byte, error) {
	if _, err := exec.LookPath("magick"); err != nil {
		return nil, fmt.Errorf("ImageMagick command 'magick' was not found: %w", err)
	}

	renderImages, cleanup := materializeDashboardCardImages(images)
	defer cleanup()

	args := []string{}
	if fontPath := deadlockCardFontPath(); fontPath != "" {
		args = append(args, "-font", fontPath)
	}
	args = append(args, "svg:-", "png:-")

	cmd := exec.CommandContext(ctx, "magick", args...)
	cmd.Stdin = strings.NewReader(buildDashboardSVG(summary, options, renderImages))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ImageMagick Deadlock card render failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// BuildDashboardSVG creates the image backing an interactive Deadlock response.
func BuildDashboardSVG(summary PlayerSummary, options CardOptions) string {
	return buildDashboardSVG(summary, options, dashboardCardImages{})
}

func loadDashboardCardImages(ctx context.Context, summary PlayerSummary, options CardOptions, loader *CardAssetLoader) dashboardCardImages {
	images := dashboardCardImages{byURL: make(map[string]string)}
	urls := dashboardCardAssetURLs(summary, options)
	if loader == nil || len(urls) == 0 {
		return images
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, rawURL := range urls {
		rawURL := rawURL
		wg.Add(1)
		go func() {
			defer wg.Done()
			if dataURI := loader.DataURI(ctx, rawURL); dataURI != "" {
				mu.Lock()
				images.byURL[rawURL] = dataURI
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return images
}

func dashboardCardAssetURLs(summary PlayerSummary, options CardOptions) []string {
	unique := make(map[string]struct{})
	urls := make([]string, 0, 7)
	add := func(rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		if _, exists := unique[rawURL]; exists {
			return
		}
		unique[rawURL] = struct{}{}
		urls = append(urls, rawURL)
	}

	switch cleanCardView(options.View) {
	case CardViewRank:
		if options.UseRankImage && summary.Rank != nil {
			add(summary.Rank.ImageURL)
		}
	case CardViewRecent:
		page, _ := recentPage(options.Page, len(summary.RecentMatches))
		start := page * recentCardPageSize
		end := minCardInt(start+recentCardPageSize, len(summary.RecentMatches))
		if start < len(summary.RecentMatches) {
			for _, match := range summary.RecentMatches[start:end] {
				add(match.HeroIconURL)
			}
		}
	case CardViewCurrent:
		if summary.CurrentGame != nil && summary.CurrentGame.Player != nil {
			add(summary.CurrentGame.Player.HeroIconURL)
		}
	case CardViewBuilds:
		if summary.Build != nil {
			add(summary.Build.HeroIconURL)
			for _, item := range summary.Build.PopularItems[:minCardInt(5, len(summary.Build.PopularItems))] {
				add(item.IconURL)
			}
		}
	case CardViewAll:
		if options.UseRankImage && summary.Rank != nil {
			add(summary.Rank.ImageURL)
		}
		for _, match := range summary.RecentMatches[:minCardInt(3, len(summary.RecentMatches))] {
			add(match.HeroIconURL)
		}
	default:
		add(summary.TopHeroIconURL)
		for _, match := range summary.RecentMatches[:minCardInt(4, len(summary.RecentMatches))] {
			add(match.HeroIconURL)
		}
	}
	return urls
}

func (i dashboardCardImages) dataURI(rawURL string) string {
	if i.byURL == nil {
		return ""
	}
	return i.byURL[rawURL]
}

func materializeDashboardCardImages(images dashboardCardImages) (dashboardCardImages, func()) {
	if len(images.byURL) == 0 {
		return images, func() {}
	}

	directory, err := os.MkdirTemp("", "deadlock-card-assets-")
	if err != nil {
		return dashboardCardImages{}, func() {}
	}
	local := dashboardCardImages{byURL: make(map[string]string, len(images.byURL))}
	index := 0
	for rawURL, dataURI := range images.byURL {
		mediaType, encoded, ok := parseCardDataURI(dataURI)
		if !ok {
			continue
		}
		payload, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(payload) == 0 {
			continue
		}
		filename := filepath.Join(directory, fmt.Sprintf("asset-%d%s", index, cardMediaExtension(mediaType)))
		index++
		if err := os.WriteFile(filename, payload, 0o600); err == nil {
			local.byURL[rawURL] = filename
		}
	}
	return local, func() { _ = os.RemoveAll(directory) }
}

func parseCardDataURI(dataURI string) (string, string, bool) {
	mediaTypeAndEncoding, encoded, ok := strings.Cut(dataURI, ",")
	if !ok || !strings.HasPrefix(mediaTypeAndEncoding, "data:") ||
		!strings.HasSuffix(mediaTypeAndEncoding, ";base64") {
		return "", "", false
	}
	mediaType := strings.TrimSuffix(strings.TrimPrefix(mediaTypeAndEncoding, "data:"), ";base64")
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp":
		return mediaType, encoded, true
	default:
		return "", "", false
	}
}

func cardMediaExtension(mediaType string) string {
	switch mediaType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

func buildDashboardSVG(summary PlayerSummary, options CardOptions, images dashboardCardImages) string {
	view := cleanCardView(options.View)
	title := cardTitle(view)
	accent := cardAccent(summary)
	status := cardStatus(summary, view)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg width="%d" height="%d" viewBox="0 0 1400 900" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">`, deadlockCardRenderWidth, deadlockCardRenderHeight)
	b.WriteString(`<defs><linearGradient id="bg" x1="0" x2="1" y1="0" y2="1"><stop stop-color="#11161a"/><stop offset="1" stop-color="#24292a"/></linearGradient><linearGradient id="header" x1="0" x2="1"><stop stop-color="#1b2124"/><stop offset="1" stop-color="#0b1013"/></linearGradient></defs>`)
	b.WriteString(`<rect width="1400" height="900" fill="url(#bg)"/><rect x="28" y="28" width="1344" height="844" rx="6" fill="#1a2023" stroke="#353c3d" stroke-width="2"/>`)
	fmt.Fprintf(&b, `<rect x="28" y="28" width="10" height="844" fill="%s"/>`, accent)
	b.WriteString(`<rect x="52" y="52" width="1294" height="132" rx="3" fill="url(#header)" stroke="#31383a"/>`)
	fmt.Fprintf(&b, `<text x="78" y="86" font-family="DejaVu Sans" font-size="16" letter-spacing="5" fill="%s">DEADLOCK // PLAYER DOSSIER</text>`, accent)
	fmt.Fprintf(&b, `<text x="78" y="130" font-family="DejaVu Sans" font-size="39" font-weight="700" fill="#eee6d8">%s</text>`, escapeCardSVG(trimCardText(summary.Name, 42)))
	fmt.Fprintf(&b, `<text x="78" y="160" font-family="DejaVu Sans" font-size="17" fill="#979b9c">STEAM ACCOUNT %d  //  %s MATCHES RECORDED</text>`, summary.AccountID, formatCardInt(summary.Matches))
	fmt.Fprintf(&b, `<rect x="1050" y="82" width="250" height="56" rx="3" fill="%s"/><text x="1175" y="118" text-anchor="middle" font-family="DejaVu Sans" font-size="19" letter-spacing="2" font-weight="700" fill="#f8f3e9">%s</text>`, accent, escapeCardSVG(title))
	fmt.Fprintf(&b, `<text x="1175" y="163" text-anchor="middle" font-family="DejaVu Sans" font-size="14" letter-spacing="1" fill="#c9bdab">%s</text>`, escapeCardSVG(status))

	switch view {
	case CardViewRank:
		writeRankCard(&b, summary, accent, images, options.UseRankImage)
	case CardViewRecent:
		writeRecentCard(&b, summary, options.Page, accent, images)
	case CardViewCurrent:
		writeCurrentCard(&b, summary, accent, images)
	case CardViewBuilds:
		writeBuildsCard(&b, summary, accent, images)
	case CardViewAll:
		writeSnapshotCard(&b, summary, accent, images, options.UseRankImage)
	default:
		writeOverviewCard(&b, summary, accent, images)
	}

	fmt.Fprintf(&b, `<text x="78" y="838" font-family="DejaVu Sans" font-size="14" letter-spacing="2" fill="%s">OVERVIEW  //  RANK  //  RECENT  //  CURRENT  //  BUILDS  //  SNAPSHOT</text>`, accent)
	b.WriteString(`<text x="1320" y="838" text-anchor="end" font-family="DejaVu Sans" font-size="14" fill="#858b8d">DATA VIA DEADLOCK API</text></svg>`)
	return b.String()
}

func writeOverviewCard(b *strings.Builder, summary PlayerSummary, accent string, images dashboardCardImages) {
	writeMetricCard(b, 60, 214, "WIN RATE", fmt.Sprintf("%.1f%%", summary.WinRate), accent)
	writeMetricCard(b, 318, 214, "RECORD", fmt.Sprintf("%d W  /  %d L", summary.Wins, summary.Losses), accent)
	writeMetricCard(b, 576, 214, "AVERAGE KDA", fmt.Sprintf("%.1f / %.1f / %.1f", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists), accent)
	writeMetricCard(b, 834, 214, "AVERAGE SOULS", formatCardInt(int(summary.AvgNetWorth)), accent)
	writeMetricCard(b, 1092, 214, "LH / DENIES", fmt.Sprintf("%.1f / %.1f", summary.AvgLastHits, summary.AvgDenies), accent)

	topHero := fallbackCardText(summary.TopHeroName, fmt.Sprintf("Hero %d", summary.TopHeroID))
	b.WriteString(`<rect x="60" y="344" width="430" height="426" rx="4" fill="#151a1d" stroke="#303739"/>`)
	fmt.Fprintf(b, `<text x="86" y="382" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">SIGNATURE HERO</text>`, accent)
	fmt.Fprintf(b, `<text x="86" y="438" font-family="DejaVu Sans" font-size="32" font-weight="700" fill="#ebe3d4">%s</text>`, escapeCardSVG(trimCardText(topHero, 13)))
	fmt.Fprintf(b, `<text x="86" y="472" font-family="DejaVu Sans" font-size="15" fill="#9c9f9d">HERO ID %d</text>`, summary.TopHeroID)
	writeCardArtwork(b, images.dataURI(summary.TopHeroIconURL), 318, 382, 142, 116)
	writeMiniMetric(b, 86, 526, "MATCHES", formatCardInt(summary.TopHeroMatches), accent)
	writeMiniMetric(b, 286, 526, "WIN RATE", fmt.Sprintf("%.1f%%", summary.TopHeroWinRate), accent)
	fmt.Fprintf(b, `<text x="86" y="676" font-family="DejaVu Sans" font-size="13" letter-spacing="2" fill="#979b9c">CAREER SAMPLE</text><text x="86" y="715" font-family="DejaVu Sans" font-size="27" font-weight="700" fill="#e6dfd2">%s GAMES</text>`, formatCardInt(summary.Matches))

	b.WriteString(`<rect x="512" y="344" width="828" height="426" rx="4" fill="#151a1d" stroke="#303739"/>`)
	fmt.Fprintf(b, `<text x="538" y="382" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">RECENT FORM</text>`, accent)
	matches := summary.RecentMatches[:minCardInt(len(summary.RecentMatches), 4)]
	if len(matches) == 0 {
		writeCardEmpty(b, 926, 554, "NO RECENT MATCHES RETURNED")
		return
	}
	for index, match := range matches {
		writeRecentRow(b, match, index, 538, 424+index*78, 770, accent, images.dataURI(match.HeroIconURL))
	}
}

func writeRankCard(b *strings.Builder, summary PlayerSummary, accent string, images dashboardCardImages, useRankImage bool) {
	b.WriteString(`<rect x="60" y="214" width="630" height="556" rx="4" fill="#151a1d" stroke="#303739"/><rect x="712" y="214" width="628" height="556" rx="4" fill="#151a1d" stroke="#303739"/>`)
	fmt.Fprintf(b, `<text x="92" y="264" font-family="DejaVu Sans" font-size="14" letter-spacing="4" fill="%s">PREDICTED RANK</text>`, accent)
	if summary.RankError != "" {
		writeCardEmpty(b, 375, 448, "RANK DATA UNAVAILABLE")
		fmt.Fprintf(b, `<text x="375" y="486" text-anchor="middle" font-family="DejaVu Sans" font-size="15" fill="#9b9f9f">%s</text>`, escapeCardSVG(trimCardText(summary.RankError, 54)))
	} else if summary.Rank == nil {
		writeCardEmpty(b, 375, 448, "RANK WAS NOT REQUESTED")
	} else {
		fmt.Fprintf(b, `<text x="92" y="354" font-family="DejaVu Sans" font-size="48" font-weight="700" fill="#eee6d8">%s</text>`, escapeCardSVG(trimCardText(summary.Rank.DisplayName(), 14)))
		fmt.Fprintf(b, `<text x="92" y="394" font-family="DejaVu Sans" font-size="16" fill="#959a9c">BADGE CODE %d</text>`, summary.Rank.Badge)
		if useRankImage {
			writeCardArtwork(b, images.dataURI(summary.Rank.ImageURL), 474, 298, 160, 160)
		}
		writeMiniMetric(b, 92, 472, "RAW SCORE", fmt.Sprintf("%.1f", summary.Rank.RawScore), accent)
		writeMiniMetric(b, 344, 472, "MATCHES USED", formatCardInt(summary.Rank.MatchesUsed), accent)
		fmt.Fprintf(b, `<text x="92" y="672" font-family="DejaVu Sans" font-size="14" fill="#8e9394">Prediction supplied by Deadlock API analytics.</text>`)
	}

	fmt.Fprintf(b, `<text x="744" y="264" font-family="DejaVu Sans" font-size="14" letter-spacing="4" fill="%s">PLAYER FORM</text>`, accent)
	writeWideMetric(b, 744, 306, "WIN RATE", fmt.Sprintf("%.1f%%", summary.WinRate), accent)
	writeWideMetric(b, 744, 412, "RECORD", fmt.Sprintf("%d W  /  %d L", summary.Wins, summary.Losses), accent)
	writeWideMetric(b, 744, 518, "SIGNATURE HERO", fallbackCardText(summary.TopHeroName, fmt.Sprintf("Hero %d", summary.TopHeroID)), accent)
	writeWideMetric(b, 744, 624, "AVERAGE KDA", fmt.Sprintf("%.1f / %.1f / %.1f", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists), accent)
}

func writeRecentCard(b *strings.Builder, summary PlayerSummary, requestedPage int, accent string, images dashboardCardImages) {
	page, pages := recentPage(requestedPage, len(summary.RecentMatches))
	fmt.Fprintf(b, `<rect x="60" y="214" width="1280" height="556" rx="4" fill="#151a1d" stroke="#303739"/><text x="90" y="258" font-family="DejaVu Sans" font-size="14" letter-spacing="4" fill="%s">RECENT MATCH LEDGER</text><text x="1308" y="258" text-anchor="end" font-family="DejaVu Sans" font-size="14" fill="#9b9f9f">PAGE %d / %d</text>`, accent, page+1, pages)
	b.WriteString(`<rect x="82" y="284" width="1236" height="42" fill="#242a2d"/><text x="102" y="311" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="#a9a392">RESULT</text><text x="300" y="311" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="#a9a392">HERO</text><text x="668" y="311" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="#a9a392">K / D / A</text><text x="885" y="311" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="#a9a392">SOULS</text><text x="1064" y="311" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="#a9a392">TIME</text><text x="1220" y="311" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="#a9a392">MATCH</text>`)

	start := page * recentCardPageSize
	end := minCardInt(start+recentCardPageSize, len(summary.RecentMatches))
	if start >= len(summary.RecentMatches) {
		writeCardEmpty(b, 700, 520, "NO RECENT MATCHES RETURNED")
		return
	}
	for index, match := range summary.RecentMatches[start:end] {
		writeRecentTableRow(b, match, 102, 374+index*72, index%2 == 0, accent, images.dataURI(match.HeroIconURL))
	}
}

func writeCurrentCard(b *strings.Builder, summary PlayerSummary, accent string, images dashboardCardImages) {
	b.WriteString(`<rect x="60" y="214" width="1280" height="556" rx="4" fill="#151a1d" stroke="#303739"/>`)
	fmt.Fprintf(b, `<text x="90" y="258" font-family="DejaVu Sans" font-size="14" letter-spacing="4" fill="%s">ACTIVE MATCH WATCH</text>`, accent)
	if summary.CurrentGameError != "" {
		writeCardEmpty(b, 700, 470, "CURRENT GAME UNAVAILABLE")
		fmt.Fprintf(b, `<text x="700" y="510" text-anchor="middle" font-family="DejaVu Sans" font-size="15" fill="#9b9f9f">%s</text>`, escapeCardSVG(trimCardText(summary.CurrentGameError, 90)))
		return
	}
	if summary.CurrentGame == nil {
		writeCardEmpty(b, 700, 470, "CURRENT GAME WAS NOT REQUESTED")
		return
	}
	status := summary.CurrentGame
	if !status.InGame || status.Match == nil || status.Player == nil {
		writeCardEmpty(b, 700, 470, "NO ACTIVE MATCH FOUND")
		b.WriteString(`<text x="700" y="512" text-anchor="middle" font-family="DejaVu Sans" font-size="15" fill="#9b9f9f">Player is not present in the current watch data.</text>`)
		return
	}

	match := status.Match
	player := status.Player
	fmt.Fprintf(b, `<rect x="90" y="290" width="294" height="62" rx="3" fill="#295a43"/><text x="237" y="329" text-anchor="middle" font-family="DejaVu Sans" font-size="19" letter-spacing="2" font-weight="700" fill="#f3ede1">CURRENTLY IN GAME</text>`)
	writeCardArtwork(b, images.dataURI(player.HeroIconURL), 294, 380, 78, 78)
	writeCurrentMetric(b, 90, 400, "HERO", activeHeroCard(player), accent)
	writeCurrentMetric(b, 90, 508, "TEAM", activeTeamCard(player), accent)
	writeCurrentMetric(b, 90, 616, "DURATION", optionalSecondsCard(match.DurationS), accent)
	writeCurrentMetric(b, 438, 292, "MATCH ID", optionalInt64Card(match.MatchID), accent)
	writeCurrentMetric(b, 438, 400, "MODE", fallbackCardText(match.MatchModeParsed, "Unknown"), accent)
	writeCurrentMetric(b, 438, 508, "REGION", fallbackCardText(match.RegionModeParsed, "Unknown"), accent)
	writeCurrentMetric(b, 438, 616, "TEAM SOULS", teamSoulsCard(match, player), accent)

	b.WriteString(`<rect x="786" y="292" width="524" height="416" rx="3" fill="#111619" stroke="#2d3436"/>`)
	fmt.Fprintf(b, `<text x="816" y="330" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">PLAYERS IN MATCH</text>`, accent)
	writeActivePlayers(b, match, 816, 372)
}

func writeBuildsCard(b *strings.Builder, summary PlayerSummary, accent string, images dashboardCardImages) {
	b.WriteString(`<rect x="60" y="214" width="1280" height="556" rx="4" fill="#151a1d" stroke="#303739"/>`)
	fmt.Fprintf(b, `<text x="90" y="258" font-family="DejaVu Sans" font-size="14" letter-spacing="4" fill="%s">BUILDS AND ITEMS</text>`, accent)
	if summary.BuildError != "" {
		writeCardEmpty(b, 700, 466, "BUILD DATA UNAVAILABLE")
		fmt.Fprintf(b, `<text x="700" y="508" text-anchor="middle" font-family="DejaVu Sans" font-size="15" fill="#9b9f9f">%s</text>`, escapeCardSVG(trimCardText(summary.BuildError, 90)))
		return
	}
	if summary.Build == nil {
		writeCardEmpty(b, 700, 466, "BUILD STATS WERE NOT REQUESTED")
		return
	}
	build := summary.Build
	fmt.Fprintf(b, `<text x="90" y="310" font-family="DejaVu Sans" font-size="31" font-weight="700" fill="#ece3d4">%s</text><text x="90" y="342" font-family="DejaVu Sans" font-size="14" fill="#959a9c">HERO ID %d  //  %s</text>`, escapeCardSVG(trimCardText(build.HeroName, 26)), build.HeroID, escapeCardSVG(trimCardText(build.BuildsSourceNote, 48)))
	writeCardArtwork(b, images.dataURI(build.HeroIconURL), 752, 272, 70, 70)
	b.WriteString(`<rect x="90" y="376" width="735" height="352" fill="#111619" stroke="#2d3436"/><rect x="850" y="376" width="460" height="352" fill="#111619" stroke="#2d3436"/>`)
	fmt.Fprintf(b, `<text x="116" y="414" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">TOP BUILDS</text><text x="876" y="414" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">POPULAR ITEMS</text>`, accent, accent)
	if len(build.Builds) == 0 {
		writeCardEmpty(b, 457, 558, "NO BUILDS RETURNED")
	} else {
		for index, row := range build.Builds[:minCardInt(4, len(build.Builds))] {
			y := 464 + index*59
			fmt.Fprintf(b, `<text x="116" y="%d" font-family="DejaVu Sans Mono" font-size="16" fill="#a09a8b">#%d</text><text x="168" y="%d" font-family="DejaVu Sans" font-size="17" fill="#e9e1d3">BUILD %d</text><text x="450" y="%d" font-family="DejaVu Sans Mono" font-size="16" fill="#c2bbad">%s MATCHES</text><text x="684" y="%d" font-family="DejaVu Sans Mono" font-size="17" fill="%s">%.1f%% WR</text>`, y, index+1, y, row.HeroBuildID, y, formatCardInt(int(row.Matches)), y, accent, row.WinRate)
		}
	}
	if len(build.PopularItems) == 0 {
		writeCardEmpty(b, 1080, 558, "NO ITEMS RETURNED")
	} else {
		for index, item := range build.PopularItems[:minCardInt(5, len(build.PopularItems))] {
			y := 464 + index*50
			writeCardArtwork(b, images.dataURI(item.IconURL), 918, y-27, 32, 32)
			fmt.Fprintf(b, `<text x="876" y="%d" font-family="DejaVu Sans Mono" font-size="15" fill="#a09a8b">#%d</text><text x="964" y="%d" font-family="DejaVu Sans" font-size="16" fill="#e9e1d3">%s</text><text x="1282" y="%d" text-anchor="end" font-family="DejaVu Sans Mono" font-size="15" fill="#bdb7a9">%s</text>`, y, index+1, y, escapeCardSVG(trimCardText(item.Name, 17)), y, formatCardInt(int(item.Builds)))
		}
	}
}

func writeSnapshotCard(b *strings.Builder, summary PlayerSummary, accent string, images dashboardCardImages, useRankImage bool) {
	writeMetricCard(b, 60, 214, "WIN RATE", fmt.Sprintf("%.1f%%", summary.WinRate), accent)
	writeMetricCard(b, 318, 214, "RECORD", fmt.Sprintf("%d W / %d L", summary.Wins, summary.Losses), accent)
	writeMetricCard(b, 576, 214, "AVERAGE KDA", fmt.Sprintf("%.1f / %.1f / %.1f", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists), accent)
	writeMetricCard(b, 834, 214, "TOP HERO", fallbackCardText(summary.TopHeroName, fmt.Sprintf("Hero %d", summary.TopHeroID)), accent)
	writeMetricCard(b, 1092, 214, "SOULS", formatCardInt(int(summary.AvgNetWorth)), accent)

	writeSnapshotPanel(b, 60, 354, 397, "RANK", snapshotRank(summary), accent)
	if useRankImage && summary.Rank != nil {
		writeCardArtwork(b, images.dataURI(summary.Rank.ImageURL), 360, 382, 70, 70)
	}
	writeSnapshotPanel(b, 481, 354, 397, "CURRENT GAME", snapshotCurrent(summary), accent)
	writeSnapshotPanel(b, 902, 354, 438, "BUILDS", snapshotBuild(summary), accent)
	fmt.Fprintf(b, `<text x="90" y="598" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">RECENT FORM</text>`, accent)
	for index, match := range summary.RecentMatches[:minCardInt(3, len(summary.RecentMatches))] {
		writeCompactRecent(b, match, 90+index*414, 646, accent, images.dataURI(match.HeroIconURL))
	}
	if len(summary.RecentMatches) == 0 {
		writeCardEmpty(b, 700, 670, "NO RECENT MATCHES RETURNED")
	}
}

func writeMetricCard(b *strings.Builder, x int, y int, label string, value string, accent string) {
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="246" height="100" rx="3" fill="#252b2e" stroke="#343a3c"/><text x="%d" y="%d" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="%s">%s</text><text x="%d" y="%d" font-family="DejaVu Sans" font-size="23" font-weight="700" fill="#ece4d6">%s</text>`, x, y, x+16, y+28, accent, escapeCardSVG(label), x+16, y+66, escapeCardSVG(trimCardText(value, 18)))
}

func writeMiniMetric(b *strings.Builder, x int, y int, label string, value string, accent string) {
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="%s">%s</text><text x="%d" y="%d" font-family="DejaVu Sans" font-size="29" font-weight="700" fill="#ece4d6">%s</text>`, x, y, accent, escapeCardSVG(label), x, y+42, escapeCardSVG(trimCardText(value, 14)))
}

func writeWideMetric(b *strings.Builder, x int, y int, label string, value string, accent string) {
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="534" height="82" rx="3" fill="#202629"/><text x="%d" y="%d" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="%s">%s</text><text x="%d" y="%d" font-family="DejaVu Sans" font-size="24" font-weight="700" fill="#ece4d6">%s</text>`, x, y, x+20, y+26, accent, escapeCardSVG(label), x+20, y+59, escapeCardSVG(trimCardText(value, 30)))
}

func writeRecentRow(b *strings.Builder, match RecentMatch, index int, x int, y int, width int, accent string, heroImage string) {
	fill := "#1d2326"
	if index%2 == 1 {
		fill = "#181e21"
	}
	result, color := "LOSS", "#b54b42"
	if match.Won {
		result, color = "WIN", "#368255"
	}
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="62" rx="3" fill="%s"/><rect x="%d" y="%d" width="78" height="62" fill="%s"/><text x="%d" y="%d" text-anchor="middle" font-family="DejaVu Sans" font-size="14" font-weight="700" fill="#f4eee2">%s</text>`, x, y, width, fill, x, y, color, x+39, y+36, result)
	writeCardArtwork(b, heroImage, x+94, y+11, 40, 40)
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="19" font-weight="700" fill="#eee7d9">%s</text><text x="%d" y="%d" font-family="DejaVu Sans Mono" font-size="16" fill="#aaa496">%d / %d / %d  //  %s SOULS</text>`, x+146, y+27, escapeCardSVG(trimCardText(fallbackCardText(match.HeroName, fmt.Sprintf("Hero %d", match.HeroID)), 20)), x+146, y+50, match.Kills, match.Deaths, match.Assists, formatCardInt(int(match.NetWorth)))
}

func writeRecentTableRow(b *strings.Builder, match RecentMatch, x int, y int, shaded bool, accent string, heroImage string) {
	if shaded {
		fmt.Fprintf(b, `<rect x="82" y="%d" width="1236" height="56" fill="#1f2528"/>`, y-35)
	}
	result, color := "LOSS", "#ba4d43"
	if match.Won {
		result, color = "WIN", "#3b8b5a"
	}
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="17" font-weight="700" fill="%s">%s</text>`, x, y, color, result)
	writeCardArtwork(b, heroImage, 248, y-28, 40, 40)
	fmt.Fprintf(b, `<text x="300" y="%d" font-family="DejaVu Sans" font-size="18" fill="#ece4d6">%s</text><text x="668" y="%d" font-family="DejaVu Sans Mono" font-size="17" fill="#c7c0b2">%d / %d / %d</text><text x="885" y="%d" font-family="DejaVu Sans Mono" font-size="17" fill="#c7c0b2">%s</text><text x="1064" y="%d" font-family="DejaVu Sans Mono" font-size="17" fill="#c7c0b2">%dm</text><text x="1220" y="%d" font-family="DejaVu Sans Mono" font-size="16" fill="%s">%d</text>`, y, escapeCardSVG(trimCardText(fallbackCardText(match.HeroName, fmt.Sprintf("Hero %d", match.HeroID)), 23)), y, match.Kills, match.Deaths, match.Assists, y, formatCardInt(int(match.NetWorth)), y, match.DurationMins, y, accent, match.MatchID)
}

func writeCurrentMetric(b *strings.Builder, x int, y int, label string, value string, accent string) {
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="12" letter-spacing="2" fill="%s">%s</text><text x="%d" y="%d" font-family="DejaVu Sans" font-size="24" font-weight="700" fill="#ece4d6">%s</text>`, x, y, accent, escapeCardSVG(label), x, y+38, escapeCardSVG(trimCardText(value, 22)))
}

func writeActivePlayers(b *strings.Builder, match *ActiveMatch, x int, y int) {
	if match == nil || len(match.Players) == 0 {
		fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="16" fill="#989d9f">No roster returned.</text>`, x, y)
		return
	}
	for index, player := range match.Players[:minCardInt(8, len(match.Players))] {
		name := fallbackCardText(player.DisplayName, optionalInt64Card(player.AccountID))
		hero := activeHeroCard(&player)
		fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="15" fill="#e6ded1">%s</text><text x="%d" y="%d" font-family="DejaVu Sans" font-size="14" fill="#999d9e">%s</text>`, x, y+index*39, escapeCardSVG(trimCardText(name, 23)), x+252, y+index*39, escapeCardSVG(trimCardText(hero, 20)))
	}
}

func writeSnapshotPanel(b *strings.Builder, x int, y int, width int, title string, value string, accent string) {
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="200" rx="3" fill="#151a1d" stroke="#303739"/><text x="%d" y="%d" font-family="DejaVu Sans" font-size="13" letter-spacing="3" fill="%s">%s</text><text x="%d" y="%d" font-family="DejaVu Sans" font-size="20" font-weight="700" fill="#e9e1d3">%s</text>`, x, y, width, x+25, y+39, accent, escapeCardSVG(title), x+25, y+89, escapeCardSVG(trimCardText(value, 29)))
}

func writeCompactRecent(b *strings.Builder, match RecentMatch, x int, y int, accent string, heroImage string) {
	result, color := "LOSS", "#ba4d43"
	if match.Won {
		result, color = "WIN", "#3b8b5a"
	}
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="390" height="96" rx="3" fill="#151a1d" stroke="#303739"/><text x="%d" y="%d" font-family="DejaVu Sans" font-size="14" font-weight="700" fill="%s">%s</text>`, x, y, x+20, y+34, color, result)
	writeCardArtwork(b, heroImage, x+78, y+12, 38, 38)
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="18" fill="#ece4d6">%s</text><text x="%d" y="%d" font-family="DejaVu Sans Mono" font-size="14" fill="%s">%d/%d/%d  //  %s SOULS</text>`, x+126, y+34, escapeCardSVG(trimCardText(fallbackCardText(match.HeroName, fmt.Sprintf("Hero %d", match.HeroID)), 15)), x+20, y+70, accent, match.Kills, match.Deaths, match.Assists, formatCardInt(int(match.NetWorth)))
}

func writeCardArtwork(b *strings.Builder, dataURI string, x int, y int, width int, height int) {
	if dataURI == "" {
		return
	}
	fmt.Fprintf(b, `<image xlink:href="%s" x="%d" y="%d" width="%d" height="%d" preserveAspectRatio="xMidYMid meet"/>`, escapeCardSVG(dataURI), x, y, width, height)
}

func writeCardEmpty(b *strings.Builder, x int, y int, message string) {
	fmt.Fprintf(b, `<text x="%d" y="%d" text-anchor="middle" font-family="DejaVu Sans" font-size="24" letter-spacing="3" fill="#bdb4a5">%s</text>`, x, y, escapeCardSVG(message))
}

func snapshotRank(summary PlayerSummary) string {
	if summary.Rank != nil {
		return summary.Rank.DisplayName()
	}
	if summary.RankError != "" {
		return "Unavailable"
	}
	return "Not requested"
}

func snapshotCurrent(summary PlayerSummary) string {
	if summary.CurrentGame != nil && summary.CurrentGame.InGame {
		return "Currently in game"
	}
	if summary.CurrentGameError != "" {
		return "Unavailable"
	}
	if summary.CurrentGame != nil {
		return "No active match"
	}
	return "Not requested"
}

func snapshotBuild(summary PlayerSummary) string {
	if summary.Build != nil {
		return fallbackCardText(summary.Build.HeroName, "Build data available")
	}
	if summary.BuildError != "" {
		return "Unavailable"
	}
	return "Not requested"
}

func activeHeroCard(player *ActiveMatchPlayer) string {
	if player == nil {
		return "Unknown"
	}
	if player.HeroName != "" {
		return player.HeroName
	}
	if player.HeroID != nil {
		return fmt.Sprintf("Hero %d", *player.HeroID)
	}
	return "Unknown"
}

func activeTeamCard(player *ActiveMatchPlayer) string {
	if player == nil {
		return "Unknown"
	}
	if strings.TrimSpace(player.TeamParsed) != "" {
		return player.TeamParsed
	}
	if player.Team != nil {
		return fmt.Sprintf("Team %d", *player.Team)
	}
	return "Unknown"
}

func teamSoulsCard(match *ActiveMatch, player *ActiveMatchPlayer) string {
	if match == nil || player == nil || player.Team == nil {
		return "Unknown"
	}
	value := match.NetWorthForTeam(*player.Team)
	if value == nil {
		return "Unknown"
	}
	return formatCardInt(int(*value))
}

func optionalSecondsCard(value *int32) string {
	if value == nil || *value < 0 {
		return "Unknown"
	}
	return fmt.Sprintf("%dm %02ds", *value/60, *value%60)
}

func optionalInt64Card(value *int64) string {
	if value == nil {
		return "Unknown"
	}
	return strconv.FormatInt(*value, 10)
}

func recentPage(requestedPage int, count int) (int, int) {
	pages := 1
	if count > 0 {
		pages = (count + recentCardPageSize - 1) / recentCardPageSize
	}
	if requestedPage < 0 {
		requestedPage = 0
	}
	if requestedPage >= pages {
		requestedPage = pages - 1
	}
	return requestedPage, pages
}

func cleanCardView(view CardView) CardView {
	switch view {
	case CardViewRank, CardViewRecent, CardViewCurrent, CardViewBuilds, CardViewAll:
		return view
	default:
		return CardViewOverview
	}
}

func cardTitle(view CardView) string {
	switch view {
	case CardViewRank:
		return "RANK PREDICTION"
	case CardViewRecent:
		return "RECENT GAMES"
	case CardViewCurrent:
		return "CURRENT GAME"
	case CardViewBuilds:
		return "BUILDS + ITEMS"
	case CardViewAll:
		return "FULL SNAPSHOT"
	default:
		return "OVERVIEW"
	}
}

func cardStatus(summary PlayerSummary, view CardView) string {
	if view == CardViewCurrent && summary.CurrentGame != nil && summary.CurrentGame.InGame {
		return "LIVE MATCH DETECTED"
	}
	if summary.Matches <= 0 {
		return "NO MATCH HISTORY"
	}
	switch {
	case summary.WinRate >= 55:
		return "WINNING FORM"
	case summary.WinRate < 45:
		return "RECOVERY FORM"
	default:
		return "EVEN FORM"
	}
}

func cardAccent(summary PlayerSummary) string {
	switch {
	case summary.WinRate >= 55:
		return "#429661"
	case summary.WinRate < 45:
		return "#b34d43"
	default:
		return "#c77736"
	}
}

func formatCardInt(value int) string {
	negative := value < 0
	if negative {
		value = -value
	}
	raw := strconv.Itoa(value)
	parts := []string{}
	for len(raw) > 3 {
		parts = append([]string{raw[len(raw)-3:]}, parts...)
		raw = raw[:len(raw)-3]
	}
	parts = append([]string{raw}, parts...)
	result := strings.Join(parts, ",")
	if negative {
		return "-" + result
	}
	return result
}

func trimCardText(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func fallbackCardText(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func minCardInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func deadlockCardFontPath() string {
	candidates := []string{
		"/usr/share/fonts/dejavu/DejaVuSans.ttf",
		"/System/Library/Fonts/Helvetica.ttc",
		"/Library/Fonts/Arial Unicode.ttf",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func escapeCardSVG(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
