package serverstats

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type CardStatus string

const (
	StatusLive     CardStatus = "LIVE"
	StatusOffline  CardStatus = "OFFLINE"
	StatusStale    CardStatus = "STALE"
	StatusClosed   CardStatus = "SESSION CLOSED"
	StatusExpired  CardStatus = "SESSION EXPIRED"
	StatusReplaced CardStatus = "SESSION REPLACED"
)

// CardView is the image-rendering model for a live or completed session.
type CardView struct {
	Endpoint    Endpoint
	Snapshot    *Snapshot
	Status      CardStatus
	Detail      string
	Page        int
	LastAttempt time.Time
	ExpiresAt   time.Time
}

// Renderer produces the scoreboard image used by Discord.
type Renderer interface {
	RenderPNG(ctx context.Context, view CardView) ([]byte, error)
}

// ImageMagickRenderer uses the runtime ImageMagick installation.
type ImageMagickRenderer struct{}

func (ImageMagickRenderer) RenderPNG(ctx context.Context, view CardView) ([]byte, error) {
	return RenderCardPNG(ctx, view)
}

// RenderCardPNG renders a Valve-industrial server scoreboard PNG.
func RenderCardPNG(ctx context.Context, view CardView) ([]byte, error) {
	if _, err := exec.LookPath("magick"); err != nil {
		return nil, fmt.Errorf("ImageMagick command 'magick' was not found: %w", err)
	}

	args := []string{}
	if fontPath := scoreboardFontPath(); fontPath != "" {
		args = append(args, "-font", fontPath)
	}
	args = append(args, "svg:-", "png:-")

	cmd := exec.CommandContext(ctx, "magick", args...)
	cmd.Stdin = strings.NewReader(BuildCardSVG(view))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ImageMagick server scoreboard render failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// BuildCardSVG creates the SVG backing the Discord scoreboard image.
func BuildCardSVG(view CardView) string {
	snapshot := view.Snapshot
	page := 0
	if snapshot != nil {
		page = snapshot.ClampPage(view.Page)
	}

	serverName := "NO RESPONSE FROM SERVER"
	gameName := "SOURCE QUERY ENDPOINT"
	mapName := "--"
	players := "--"
	bots := "--"
	latency := "--"
	vac := "--"
	password := "--"
	if snapshot != nil {
		serverName = fallback(snapshot.Name, "UNNAMED SERVER")
		gameName = fallback(snapshot.Game, "SOURCE SERVER")
		mapName = fallback(snapshot.Map, "Unknown")
		players = fmt.Sprintf("%d / %d", snapshot.CurrentPlayers, snapshot.MaxPlayers)
		bots = fmt.Sprintf("%d", snapshot.Bots)
		latency = fmt.Sprintf("%d ms", snapshot.Latency.Milliseconds())
		vac = yesNo(snapshot.VAC)
		password = yesNo(snapshot.Password)
	}

	statusColor := statusAccent(view.Status)
	statusText := string(view.Status)
	if statusText == "" {
		statusText = string(StatusOffline)
	}

	var b strings.Builder
	b.WriteString(`<svg width="1400" height="900" viewBox="0 0 1400 900" xmlns="http://www.w3.org/2000/svg">`)
	b.WriteString(`<defs><linearGradient id="background" x1="0" x2="1" y1="0" y2="1"><stop stop-color="#171a1c"/><stop offset="1" stop-color="#292c2e"/></linearGradient><linearGradient id="header" x1="0" x2="1"><stop stop-color="#25282a"/><stop offset="1" stop-color="#111314"/></linearGradient></defs>`)
	b.WriteString(`<rect width="1400" height="900" fill="url(#background)"/><rect x="28" y="28" width="1344" height="844" rx="6" fill="#202325" stroke="#3a3d40" stroke-width="2"/>`)
	b.WriteString(`<rect x="28" y="28" width="10" height="844" fill="#d46a23"/><rect x="52" y="52" width="1294" height="132" rx="3" fill="url(#header)" stroke="#333638"/>`)
	fmt.Fprintf(&b, `<text x="78" y="86" font-family="DejaVu Sans, Arial, sans-serif" font-size="16" letter-spacing="5" fill="#d46a23">VALVE SERVER MONITOR</text>`)
	fmt.Fprintf(&b, `<text x="78" y="128" font-family="DejaVu Sans, Arial, sans-serif" font-size="36" font-weight="700" fill="#f2f0ea">%s</text>`, escapeSVG(trimText(serverName, 54)))
	fmt.Fprintf(&b, `<text x="78" y="160" font-family="DejaVu Sans, Arial, sans-serif" font-size="18" fill="#a7aaab">%s  //  %s</text>`, escapeSVG(trimText(gameName, 34)), escapeSVG(view.Endpoint.Address))
	fmt.Fprintf(&b, `<rect x="1080" y="84" width="222" height="54" rx="3" fill="%s"/><text x="1191" y="119" text-anchor="middle" font-family="DejaVu Sans, Arial, sans-serif" font-size="21" font-weight="700" fill="#f8f5ef">%s</text>`, statusColor, escapeSVG(statusText))
	if view.Status == StatusStale && snapshot != nil {
		fmt.Fprintf(&b, `<text x="1191" y="163" text-anchor="middle" font-family="DejaVu Sans, Arial, sans-serif" font-size="13" letter-spacing="1" fill="#c87539">LAST GOOD %s - RETRYING</text>`, escapeSVG(formatCardTime(snapshot.FetchedAt)))
	}

	writeMetric(&b, 60, "MAP", mapName)
	writeMetric(&b, 270, "PLAYERS", players)
	writeMetric(&b, 480, "PING", latency)
	writeMetric(&b, 690, "VAC", vac)
	writeMetric(&b, 900, "PASSWORD", password)
	writeMetric(&b, 1110, "BOTS", bots)

	b.WriteString(`<rect x="60" y="292" width="1280" height="480" rx="4" fill="#171a1b" stroke="#34383a"/>`)
	b.WriteString(`<rect x="60" y="292" width="1280" height="48" fill="#2b2f31"/>`)
	b.WriteString(`<text x="90" y="323" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" font-weight="700" letter-spacing="2" fill="#c87539">#</text><text x="152" y="323" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" font-weight="700" letter-spacing="2" fill="#c87539">PLAYER</text><text x="1080" y="323" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" font-weight="700" letter-spacing="2" fill="#c87539">SCORE</text><text x="1220" y="323" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" font-weight="700" letter-spacing="2" fill="#c87539">CONNECTED</text>`)

	writeRoster(&b, snapshot, page, view.Detail)

	pageText := "PAGE 1 / 1"
	if snapshot != nil {
		pageText = fmt.Sprintf("PAGE %d / %d", page+1, snapshot.PageCount())
	}
	lastRefresh := formatCardTime(view.LastAttempt)
	expiry := formatCardTime(view.ExpiresAt)
	fmt.Fprintf(&b, `<text x="78" y="826" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" letter-spacing="2" fill="#d46a23">%s</text>`, escapeSVG(pageText))
	fmt.Fprintf(&b, `<text x="690" y="826" text-anchor="middle" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" fill="#979a9c">LAST QUERY  %s</text>`, escapeSVG(lastRefresh))
	fmt.Fprintf(&b, `<text x="1322" y="826" text-anchor="end" font-family="DejaVu Sans, Arial, sans-serif" font-size="15" fill="#979a9c">SESSION ENDS  %s</text>`, escapeSVG(expiry))

	if isTerminalStatus(view.Status) {
		b.WriteString(`<rect x="52" y="52" width="1294" height="720" fill="#111314" opacity="0.48"/>`)
		fmt.Fprintf(&b, `<rect x="440" y="396" width="520" height="94" rx="4" fill="#202325" stroke="%s" stroke-width="3"/><text x="700" y="453" text-anchor="middle" font-family="DejaVu Sans, Arial, sans-serif" font-size="28" letter-spacing="3" font-weight="700" fill="#f2f0ea">%s</text>`, statusColor, escapeSVG(statusText))
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func writeMetric(b *strings.Builder, x int, label string, value string) {
	fmt.Fprintf(b, `<rect x="%d" y="204" width="192" height="66" rx="3" fill="#292c2e" stroke="#35393b"/><text x="%d" y="228" font-family="DejaVu Sans, Arial, sans-serif" font-size="12" letter-spacing="2" fill="#9b9e9f">%s</text><text x="%d" y="256" font-family="DejaVu Sans, Arial, sans-serif" font-size="20" font-weight="700" fill="#ece9e3">%s</text>`, x, x+16, escapeSVG(label), x+16, escapeSVG(trimText(value, 18)))
}

func writeRoster(b *strings.Builder, snapshot *Snapshot, page int, detail string) {
	if snapshot == nil {
		message := "Retrying automatically."
		if strings.TrimSpace(detail) != "" {
			message += " " + detail
		}
		writeEmptyRoster(b, "SERVER UNREACHABLE", message)
		return
	}
	if !snapshot.RosterAvailable {
		writeEmptyRoster(b, "ROSTER UNAVAILABLE", "The server reports player totals but does not expose player rows.")
		return
	}

	rows := snapshot.RosterPage(page)
	if len(rows) == 0 {
		writeEmptyRoster(b, "SERVER IS EMPTY", "No visible players are connected.")
		return
	}

	for index, player := range rows {
		y := 378 + index*39
		if index%2 == 0 {
			fmt.Fprintf(b, `<rect x="72" y="%d" width="1256" height="38" fill="#202426"/>`, y-27)
		}
		number := page*RowsPerPage + index + 1
		fmt.Fprintf(b, `<text x="90" y="%d" font-family="DejaVu Sans Mono, monospace" font-size="17" fill="#8e9293">%02d</text>`, y, number)
		fmt.Fprintf(b, `<text x="152" y="%d" font-family="DejaVu Sans, Arial, sans-serif" font-size="18" fill="#f0ede8">%s</text>`, y, escapeSVG(trimText(fallback(player.Name, "Unnamed Player"), 56)))
		fmt.Fprintf(b, `<text x="1130" y="%d" text-anchor="end" font-family="DejaVu Sans Mono, monospace" font-size="18" fill="#e2dfd9">%d</text>`, y, player.Score)
		fmt.Fprintf(b, `<text x="1296" y="%d" text-anchor="end" font-family="DejaVu Sans Mono, monospace" font-size="18" fill="#b2b5b6">%s</text>`, y, escapeSVG(formatDuration(player.Connected)))
	}
}

func writeEmptyRoster(b *strings.Builder, heading string, detail string) {
	fmt.Fprintf(b, `<text x="700" y="486" text-anchor="middle" font-family="DejaVu Sans, Arial, sans-serif" font-size="28" letter-spacing="3" fill="#e7e4dd">%s</text>`, escapeSVG(heading))
	fmt.Fprintf(b, `<text x="700" y="526" text-anchor="middle" font-family="DejaVu Sans, Arial, sans-serif" font-size="17" fill="#989c9e">%s</text>`, escapeSVG(trimText(detail, 106)))
}

func statusAccent(status CardStatus) string {
	switch status {
	case StatusLive:
		return "#31814c"
	case StatusOffline, StatusStale:
		return "#a34234"
	case StatusClosed, StatusExpired, StatusReplaced:
		return "#d46a23"
	default:
		return "#a34234"
	}
}

func isTerminalStatus(status CardStatus) bool {
	return status == StatusClosed || status == StatusExpired || status == StatusReplaced
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	totalSeconds := int(duration.Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	}
	return fmt.Sprintf("%02dm %02ds", minutes, seconds)
}

func formatCardTime(value time.Time) string {
	if value.IsZero() {
		return "--:--:-- UTC"
	}
	return value.UTC().Format("15:04:05 UTC")
}

func trimText(value string, limit int) string {
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

func fallback(value string, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}

func yesNo(value bool) string {
	if value {
		return "YES"
	}
	return "NO"
}

func scoreboardFontPath() string {
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

func escapeSVG(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}
