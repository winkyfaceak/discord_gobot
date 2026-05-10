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
	topHero := escapeSVG(summary.TopHeroName)
	if topHero == "" {
		topHero = fmt.Sprintf("Hero ID %d", summary.TopHeroID)
	}

	return fmt.Sprintf(`
<svg width="1200" height="675" viewBox="0 0 1200 675" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <linearGradient id="paper" x1="0" x2="1" y1="0" y2="1">
      <stop offset="0%%" stop-color="#e4d6b4"/>
      <stop offset="100%%" stop-color="#c9b287"/>
    </linearGradient>

    <linearGradient id="inkwash" x1="0" x2="1" y1="0" y2="0">
      <stop offset="0%%" stop-color="#2c1d12"/>
      <stop offset="100%%" stop-color="#5a3f28"/>
    </linearGradient>

    <filter id="shadow" x="-20%%" y="-20%%" width="140%%" height="140%%">
      <feDropShadow dx="0" dy="12" stdDeviation="10" flood-color="#000000" flood-opacity="0.18"/>
    </filter>
  </defs>

  <rect width="1200" height="675" fill="#b3976a"/>
  <rect x="24" y="24" width="1152" height="627" rx="18" fill="url(#paper)" stroke="#4b3320" stroke-width="4"/>
  <rect x="45" y="45" width="1110" height="585" rx="12" fill="none" stroke="#7b5a36" stroke-width="2" stroke-dasharray="2 8"/>

  <rect x="72" y="70" width="1056" height="112" rx="12" fill="url(#inkwash)" filter="url(#shadow)"/>
  <text x="96" y="122" font-family="Georgia, 'Times New Roman', serif" font-size="28" letter-spacing="3" fill="#e5c88b">DEADLOCK DOSSIER</text>
  <text x="96" y="160" font-family="Georgia, 'Times New Roman', serif" font-size="42" font-weight="700" fill="#fbf3e4">%s</text>
  <text x="826" y="122" font-family="Georgia, 'Times New Roman', serif" font-size="18" text-anchor="end" fill="#d8c7a4">COMBAT LEDGER</text>
  <text x="1100" y="160" font-family="Georgia, 'Times New Roman', serif" font-size="20" text-anchor="end" fill="#f3e6cb">ACCOUNT ID %d</text>

  <line x1="95" y1="212" x2="1105" y2="212" stroke="#6d4c2d" stroke-width="3"/>
  <line x1="95" y1="220" x2="1105" y2="220" stroke="#b89058" stroke-width="1"/>

  %s

  <rect x="84" y="558" width="1032" height="52" rx="10" fill="#efe3c8" stroke="#8a6a44" stroke-width="2"/>
  <text x="108" y="592" font-family="Georgia, 'Times New Roman', serif" font-size="24" fill="#3f2b1a">Top Hero: %s</text>
  <text x="1095" y="592" font-family="Georgia, 'Times New Roman', serif" font-size="22" text-anchor="end" fill="#5a422b">%d matches • %.1f%% WR</text>

  <text x="600" y="640" text-anchor="middle" font-family="Georgia, 'Times New Roman', serif" font-size="17" letter-spacing="1.2" fill="#60472e">Filed in a retro bureau style. Use the Discord buttons for the full dossier.</text>
</svg>
`,
		name,
		summary.AccountID,
		buildStatTilesSVG(summary),
		topHero,
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
		{"MATCHES", fmt.Sprintf("%d", summary.Matches), 96, 256},
		{"SUCCESS RATE", fmt.Sprintf("%.1f%%", summary.WinRate), 366, 256},
		{"WINS / LOSSES", fmt.Sprintf("%d / %d", summary.Wins, summary.Losses), 636, 256},
		{"AVERAGE K / D / A", fmt.Sprintf("%.1f / %.1f / %.1f", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists), 96, 404},
		{"AVERAGE SOULS", fmt.Sprintf("%.0f", summary.AvgNetWorth), 366, 404},
		{"LH / DENIES", fmt.Sprintf("%.1f / %.1f", summary.AvgLastHits, summary.AvgDenies), 636, 404},
	}

	var b strings.Builder

	for _, tile := range tiles {
		b.WriteString(fmt.Sprintf(`
  <rect x="%d" y="%d" width="238" height="116" rx="12" fill="#f2e7cf" stroke="#8d6d46" stroke-width="2"/>
  <rect x="%d" y="%d" width="238" height="24" rx="12" fill="#7a5534" opacity="0.95"/>
  <text x="%d" y="%d" font-family="Georgia, 'Times New Roman', serif" font-size="16" letter-spacing="1.6" fill="#f8ecd6">%s</text>
  <text x="%d" y="%d" font-family="Georgia, 'Times New Roman', serif" font-size="34" font-weight="700" fill="#352315">%s</text>
`,
			tile.x,
			tile.y,
			tile.x,
			tile.y,
			tile.x+16,
			tile.y+17,
			escapeSVG(tile.label),
			tile.x+16,
			tile.y+76,
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
