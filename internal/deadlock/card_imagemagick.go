package deadlock

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"strings"
)

// RenderCardPNG renders a Deadlock statistics PNG using ImageMagick.
func RenderCardPNG(ctx context.Context, summary PlayerSummary) ([]byte, error) {
	if _, err := exec.LookPath("magick"); err != nil {
		return nil, fmt.Errorf("ImageMagick command 'magick' was not found: %w", err)
	}

	svg := buildStatsSVG(summary)

	cmd := exec.CommandContext(ctx, "magick", "svg:-", "png:-")
	cmd.Stdin = strings.NewReader(svg)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ImageMagick render failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

func buildStatsSVG(summary PlayerSummary) string {
	name := escapeSVG(summary.Name)

	return fmt.Sprintf(`
<svg width="1200" height="675" viewBox="0 0 1200 675" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="bg" x1="0" x2="1" y1="0" y2="1">
      <stop offset="0%%" stop-color="#101827"/>
      <stop offset="100%%" stop-color="#2b1b10"/>
    </linearGradient>

    <filter id="shadow" x="-20%%" y="-20%%" width="140%%" height="140%%">
      <feDropShadow dx="0" dy="10" stdDeviation="14" flood-color="#000000" flood-opacity="0.35"/>
    </filter>
  </defs>

  <rect width="1200" height="675" fill="url(#bg)"/>
  <circle cx="1050" cy="110" r="180" fill="#ff9f1c" opacity="0.12"/>
  <circle cx="130" cy="590" r="220" fill="#62d6ff" opacity="0.10"/>

  <rect x="60" y="55" width="1080" height="565" rx="34" fill="#101010" opacity="0.72" filter="url(#shadow)"/>
  <rect x="90" y="85" width="1020" height="505" rx="26" fill="#171a21" opacity="0.95"/>

  <text x="120" y="145" font-family="Arial, Helvetica, sans-serif" font-size="54" font-weight="800" fill="#ffffff">%s</text>
  <text x="120" y="188" font-family="Arial, Helvetica, sans-serif" font-size="24" fill="#b8c1d1">Deadlock Statistics • Account ID %d</text>

  %s

  <text x="120" y="545" font-family="Arial, Helvetica, sans-serif" font-size="23" fill="#d8dee9">Top Hero ID: %d • %d matches • %.1f%% WR</text>
  <text x="120" y="580" font-family="Arial, Helvetica, sans-serif" font-size="18" fill="#8792a2">Hero-name lookup can be added next through the assets API.</text>
</svg>
`,
		name,
		summary.AccountID,
		buildStatTilesSVG(summary),
		summary.TopHeroID,
		summary.TopHeroMatches,
		summary.TopHeroWinRate,
	)
}

func buildStatTilesSVG(summary PlayerSummary) string {
	tiles := []struct {
		label string
		value string
		x     int
		y     int
	}{
		{"Matches", fmt.Sprintf("%d", summary.Matches), 120, 245},
		{"Win Rate", fmt.Sprintf("%.1f%%", summary.WinRate), 385, 245},
		{"W / L", fmt.Sprintf("%d / %d", summary.Wins, summary.Losses), 650, 245},
		{"Avg KDA", fmt.Sprintf("%.1f / %.1f / %.1f", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists), 120, 385},
		{"Avg Souls", fmt.Sprintf("%.0f", summary.AvgNetWorth), 385, 385},
		{"LH / Denies", fmt.Sprintf("%.1f / %.1f", summary.AvgLastHits, summary.AvgDenies), 650, 385},
	}

	var b strings.Builder

	for _, tile := range tiles {
		b.WriteString(fmt.Sprintf(`
  <rect x="%d" y="%d" width="230" height="105" rx="22" fill="#242936"/>
  <text x="%d" y="%d" font-family="Arial, Helvetica, sans-serif" font-size="20" fill="#96a0b5">%s</text>
  <text x="%d" y="%d" font-family="Arial, Helvetica, sans-serif" font-size="34" font-weight="800" fill="#ffffff">%s</text>
`,
			tile.x,
			tile.y,
			tile.x+24,
			tile.y+38,
			escapeSVG(tile.label),
			tile.x+24,
			tile.y+78,
			escapeSVG(tile.value),
		))
	}

	return b.String()
}

func escapeSVG(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
