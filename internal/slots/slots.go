// Package slots is a three-reel slot machine: weighted reels, the payout
// table, and drawings of the machine (an animated spin and the result).
package slots

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/render"
)

// Symbol is one reel symbol: how often it comes up, what three of a kind
// pays per coin bet, and how to draw it centred on (x, y).
type Symbol struct {
	Name   string
	Emoji  string
	Weight int
	Three  int64
	draw   func(b *strings.Builder, x, y int)
}

// Symbols, most common first. TestSlotsKeepASmallHouseEdge checks the table
// pays back a little under 100% on average.
var Symbols = []Symbol{
	{"Cherry", "🍒", 8, 5, drawCherry},
	{"Lemon", "🍋", 7, 8, drawLemon},
	{"Orange", "🍊", 6, 10, drawOrange},
	{"Grapes", "🍇", 5, 15, drawGrapes},
	{"Bell", "🔔", 3, 40, drawBell},
	{"Diamond", "💎", 2, 100, drawDiamond},
	{"Seven", "7️⃣", 1, 500, drawSeven},
}

const cherry = 0

// Spin picks each reel's symbol, weighted by how common it is.
func Spin() [3]int {
	total := 0
	for _, s := range Symbols {
		total += s.Weight
	}
	var reels [3]int
	for r := range reels {
		n := rand.IntN(total)
		for i, s := range Symbols {
			if n < s.Weight {
				reels[r] = i
				break
			}
			n -= s.Weight
		}
	}
	return reels
}

// Multiplier is what a spin pays per coin bet: three of a kind pays its
// symbol's multiplier, two cherries pay 2×, any other pair returns the bet,
// and anything else loses it.
func Multiplier(reels [3]int) int64 {
	a, b, c := reels[0], reels[1], reels[2]
	switch {
	case a == b && b == c:
		return Symbols[a].Three
	case a == b || a == c:
		return pair(a)
	case b == c:
		return pair(b)
	}
	return 0
}

func pair(symbol int) int64 {
	if symbol == cherry {
		return 2
	}
	return 1
}

// Describe names the result, e.g. "Three Bells ×40".
func Describe(reels [3]int) string {
	m := Multiplier(reels)
	switch {
	case m >= 100:
		return fmt.Sprintf("JACKPOT! Three %ss ×%d", Symbols[reels[0]].Name, m)
	case m > 2:
		return fmt.Sprintf("Three %ss ×%d", Symbols[reels[0]].Name, m)
	case m == 2:
		return "Cherry pair ×2"
	case m == 1:
		return "A pair: bet back"
	default:
		return "No win"
	}
}

const (
	width, height = 600, 380
	cell          = 100 // height of one symbol on a reel
	windowWidth   = 140
	windowHeight  = 180
	windowTop     = 100
	frameBorder   = 4
	frames        = 34
	frameDelay    = 6 // hundredths of a second
)

var windowLeft = [3]int{85, 230, 375}

// SpinDuration is how long the GIF takes to reach the final frame.
const SpinDuration = (frames - 1) * frameDelay * 10 * time.Millisecond

func stripHeight() int { return len(Symbols) * cell }

// interior is the part of reel r's window that shows symbols.
func interior(r int) image.Rectangle {
	return image.Rect(windowLeft[r]+frameBorder, windowTop+frameBorder, windowLeft[r]+windowWidth-frameBorder, windowTop+windowHeight-frameBorder)
}

// restingOffset is how far down the reel strip the window starts when
// symbol sits in the middle, on the payline.
func restingOffset(symbol int) int {
	middle := (windowHeight - 2*frameBorder) / 2
	return ((cell/2+symbol*cell-middle)%stripHeight() + stripHeight()) % stripHeight()
}

// art is what every spin reuses: the machine with empty windows, one reel
// strip, and the GIF colours.
type art struct {
	machine *image.RGBA
	strip   *image.RGBA
	palette *render.Palette
}

var (
	artMu    sync.Mutex
	artCache *art
)

func cachedArt(ctx context.Context) (*art, error) {
	artMu.Lock()
	defer artMu.Unlock()
	if artCache != nil {
		return artCache, nil
	}
	machine, err := render.RGBA(ctx, machineSVG(nil, ""))
	if err != nil {
		return nil, err
	}
	strip, err := render.RGBA(ctx, stripSVG())
	if err != nil {
		return nil, err
	}
	// These reels show every symbol at least in part, so all their colours count
	palette, err := render.NewPalette(ctx, machineSVG(&[3]int{1, 3, 5}, ""))
	if err != nil {
		return nil, err
	}
	artCache = &art{machine: machine, strip: strip, palette: palette}
	return artCache, nil
}

// RenderSpin returns an animated GIF of the reels spinning and stopping one
// at a time on reels, and a PNG of the result with what it paid.
func RenderSpin(ctx context.Context, reels [3]int) (gifData []byte, still []byte, err error) {
	a, err := cachedArt(ctx)
	if err != nil {
		return nil, nil, err
	}

	// One frame buffer: each frame only repaints the reel windows
	frame := image.NewRGBA(a.machine.Bounds())
	draw.Draw(frame, frame.Bounds(), a.machine, image.Point{}, draw.Src)
	reelArea := interior(0).Union(interior(2))
	stops := [3]float64{0.5, 0.72, 0.94}
	turns := [3]float64{3, 4, 5}

	anim := &gif.GIF{LoopCount: -1} // play once and stay on the result
	for i := range frames {
		t := float64(i) / float64(frames-1)
		for r := range reels {
			u := math.Min(t/stops[r], 1)
			offset := restingOffset(reels[r]) + int(turns[r]*float64(stripHeight())*(1-u)*(1-u))
			paintReel(frame, a.strip, interior(r), offset)
		}
		area, delay := reelArea, frameDelay
		if i == 0 {
			area = frame.Bounds()
		}
		if i == frames-1 {
			delay = 200
		}
		anim.Image = append(anim.Image, a.palette.Quantize(frame, area))
		anim.Delay = append(anim.Delay, delay)
		anim.Disposal = append(anim.Disposal, gif.DisposalNone)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, anim); err != nil {
		return nil, nil, err
	}

	still, err = render.PNG(ctx, machineSVG(&reels, Describe(reels)))
	return out.Bytes(), still, err
}

// paintReel copies the strip into a window, starting offset pixels down the
// strip and wrapping round its end.
func paintReel(frame, strip *image.RGBA, window image.Rectangle, offset int) {
	rowBytes := window.Dx() * 4
	for y := 0; y < window.Dy(); y++ {
		src := strip.PixOffset(0, (offset+y)%stripHeight())
		dst := frame.PixOffset(window.Min.X, window.Min.Y+y)
		copy(frame.Pix[dst:dst+rowBytes], strip.Pix[src:src+rowBytes])
	}
}

// stripSVG draws one reel's symbols top to bottom, as wide as a window.
func stripSVG() string {
	w := windowWidth - 2*frameBorder
	var b strings.Builder
	fmt.Fprintf(&b, `<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`, w, stripHeight(), w, stripHeight())
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fbf6e9"/>`, w, stripHeight())
	for i, s := range Symbols {
		s.draw(&b, w/2, cell/2+i*cell)
		fmt.Fprintf(&b, `<rect x="0" y="%d" width="%d" height="2" fill="#e6dcc2"/>`, (i+1)*cell-1, w)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// machineSVG draws the machine. With reels it shows them stopped on the
// payline, and banner (if any) shows the result underneath.
func machineSVG(reels *[3]int, banner string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`, width, height, width, height)
	b.WriteString(`<defs><linearGradient id="cabinet" x1="0" x2="0" y1="0" y2="1"><stop stop-color="#a52335"/><stop offset="1" stop-color="#5c0f1a"/></linearGradient>`)
	for r := range 3 {
		in := interior(r)
		fmt.Fprintf(&b, `<clipPath id="window%d"><rect x="%d" y="%d" width="%d" height="%d"/></clipPath>`, r, in.Min.X, in.Min.Y, in.Dx(), in.Dy())
	}
	b.WriteString(`</defs>`)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#13181b"/>`, width, height)
	b.WriteString(`<rect x="20" y="20" width="560" height="340" rx="24" fill="url(#cabinet)" stroke="#c9a227" stroke-width="6"/>`)
	b.WriteString(`<rect x="150" y="34" width="300" height="46" rx="10" fill="#1c1c1c" stroke="#c9a227" stroke-width="3"/>`)
	b.WriteString(`<text x="300" y="65" text-anchor="middle" font-family="DejaVu Sans" font-size="24" font-weight="700" letter-spacing="4" fill="#f2d16b">WINKY SLOTS</text>`)

	for r := range 3 {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" rx="8" fill="#fbf6e9" stroke="#c9a227" stroke-width="%d"/>`,
			windowLeft[r], windowTop, windowWidth, windowHeight, 2*frameBorder)
		if reels != nil {
			in := interior(r)
			// The strip twice over, so the window can wrap past its end
			fmt.Fprintf(&b, `<g clip-path="url(#window%d)"><g transform="translate(%d %d)">`, r, in.Min.X, in.Min.Y-restingOffset(reels[r]))
			for copyAt := range 2 {
				for i, s := range Symbols {
					s.draw(&b, in.Dx()/2, cell/2+i*cell+copyAt*stripHeight())
				}
			}
			b.WriteString(`</g></g>`)
		}
	}
	// Payline markers either side of the reels
	mid := windowTop + windowHeight/2
	fmt.Fprintf(&b, `<path d="M58 %d l18 -12 v24 z" fill="#f2d16b"/><path d="M542 %d l-18 -12 v24 z" fill="#f2d16b"/>`, mid, mid)

	if banner != "" {
		fill := "#8f1d2c"
		if m := Multiplier(*reels); m >= 2 {
			fill = "#1f7a3a"
		} else if m == 1 {
			fill = "#7a6a1f"
		}
		fmt.Fprintf(&b, `<rect x="120" y="296" width="360" height="46" rx="10" fill="%s" stroke="#f2d16b" stroke-width="2"/>`, fill)
		fmt.Fprintf(&b, `<text x="300" y="326" text-anchor="middle" font-family="DejaVu Sans" font-size="19" font-weight="700" fill="#fff">%s</text>`, render.Escape(strings.ToUpper(banner)))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func drawCherry(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<path d="M%d %d Q%d %d %d %d M%d %d Q%d %d %d %d" stroke="#2e7d32" stroke-width="4" fill="none" stroke-linecap="round"/>`,
		x-12, y+6, x-6, y-18, x+6, y-30, x+16, y+2, x+14, y-18, x+6, y-30)
	fmt.Fprintf(b, `<ellipse cx="%d" cy="%d" rx="13" ry="6" fill="#43a047" transform="rotate(-25 %d %d)"/>`, x+17, y-29, x+17, y-29)
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="16" fill="#c62828"/><circle cx="%d" cy="%d" r="16" fill="#e53935"/>`, x-12, y+18, x+16, y+14)
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="4" fill="#ffcdd2"/><circle cx="%d" cy="%d" r="4" fill="#ffcdd2"/>`, x-17, y+12, x+11, y+8)
}

func drawLemon(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="6" fill="#f9a825"/><circle cx="%d" cy="%d" r="6" fill="#f9a825"/>`, x-31, y, x+31, y)
	fmt.Fprintf(b, `<ellipse cx="%d" cy="%d" rx="32" ry="24" fill="#fdd835" stroke="#f9a825" stroke-width="3"/>`, x, y)
	fmt.Fprintf(b, `<ellipse cx="%d" cy="%d" rx="11" ry="5" fill="#fff9c4"/>`, x-10, y-10)
}

func drawOrange(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="29" fill="#fb8c00" stroke="#e65100" stroke-width="3"/>`, x, y+2)
	fmt.Fprintf(b, `<ellipse cx="%d" cy="%d" rx="10" ry="5" fill="#43a047" transform="rotate(-20 %d %d)"/>`, x+8, y-28, x+8, y-28)
	fmt.Fprintf(b, `<ellipse cx="%d" cy="%d" rx="9" ry="5" fill="#ffcc80"/>`, x-11, y-9)
}

func drawGrapes(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<path d="M%d %d q2 -12 10 -16" stroke="#2e7d32" stroke-width="4" fill="none" stroke-linecap="round"/>`, x, y-20)
	for _, at := range [][2]int{{-16, -10}, {0, -10}, {16, -10}, {-8, 6}, {8, 6}, {0, 22}} {
		fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="11" fill="#7b1fa2" stroke="#4a148c" stroke-width="2"/>`, x+at[0], y+at[1])
	}
}

func drawBell(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<circle cx="%d" cy="%d" r="5" fill="#f57f17"/>`, x, y-31)
	fmt.Fprintf(b, `<path d="M%d %d Q%d %d %d %d Q%d %d %d %d Z" fill="#fbc02d" stroke="#f57f17" stroke-width="3"/>`,
		x-27, y+18, x-25, y-28, x, y-28, x+25, y-28, x+27, y+18)
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="62" height="9" rx="4" fill="#f9a825" stroke="#f57f17" stroke-width="2"/><circle cx="%d" cy="%d" r="6" fill="#f57f17"/>`,
		x-31, y+15, x, y+29)
}

func drawDiamond(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<polygon points="%d,%d %d,%d %d,%d %d,%d %d,%d" fill="#4fc3f7" stroke="#0277bd" stroke-width="3" stroke-linejoin="round"/>`,
		x-30, y-10, x-16, y-26, x+16, y-26, x+30, y-10, x, y+30)
	fmt.Fprintf(b, `<path d="M%d %d H%d M%d %d L%d %d L%d %d M%d %d L%d %d L%d %d" stroke="#0277bd" stroke-width="2" fill="none"/>`,
		x-30, y-10, x+30, x-16, y-26, x-8, y-10, x, y+30, x+16, y-26, x+8, y-10, x, y+30)
}

func drawSeven(b *strings.Builder, x, y int) {
	fmt.Fprintf(b, `<text x="%d" y="%d" text-anchor="middle" font-family="DejaVu Sans" font-size="84" font-weight="700" fill="#d32f2f" stroke="#7f0000" stroke-width="3">7</text>`, x, y+30)
}
