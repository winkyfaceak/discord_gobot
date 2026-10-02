// Package roulette draws a European roulette wheel: an animated GIF of a spin
// landing on a given number, and a still image of where it stopped.
package roulette

import (
	"bytes"
	"context"
	"discord_gobot/internal/render"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
)

// WheelOrder is a European (single zero) wheel's pockets, clockwise from 0.
var WheelOrder = [37]int{
	0, 32, 15, 19, 4, 21, 2, 25, 17, 34, 6, 27, 13, 36, 11, 30, 8, 23, 10,
	5, 24, 16, 33, 1, 20, 14, 31, 9, 22, 18, 29, 7, 28, 12, 35, 3, 26,
}

var redNumbers = []int{1, 3, 5, 7, 9, 12, 14, 16, 18, 19, 21, 23, 25, 27, 30, 32, 34, 36}

// Color is "green" for 0, otherwise "red" or "black".
func Color(n int) string {
	switch {
	case n == 0:
		return "green"
	case slices.Contains(redNumbers, n):
		return "red"
	default:
		return "black"
	}
}

const (
	size       = 400
	center     = size / 2
	frames     = 48 // at 7/100 s each: a 3.4 second spin
	frameDelay = 7
	step       = 360.0 / 37

	pocketOuter = 176.0
	pocketInner = 132.0
	numberAt    = 163.0
	ballTrack   = 182.0 // where the ball circles before it drops
	ballRest    = 142.0 // where it sits in a pocket
	ballSize    = 7.0
)

// SpinDuration is how long the GIF takes to reach the final frame.
const SpinDuration = (frames - 1) * frameDelay * 10 * time.Millisecond

// RenderSpin returns an animated GIF of the wheel spinning and the ball
// landing on result, and a PNG of the final position with the result shown
// in the middle.
//
// The wheel is drawn once by ImageMagick and cached; each GIF frame rotates
// that picture and adds the ball in Go, which takes a fraction of a second
// and a few MB instead of rendering 48 SVGs.
func RenderSpin(ctx context.Context, result int) (gifData []byte, still []byte, err error) {
	if !slices.Contains(WheelOrder[:], result) {
		return nil, nil, fmt.Errorf("no pocket %d on the wheel", result)
	}
	base, err := cachedWheel(ctx)
	if err != nil {
		return nil, nil, err
	}

	anim := &gif.GIF{LoopCount: -1} // play once and stay on where it stopped
	for i := range frames {
		wheel, ballAngle, ballRadius := Positions(result, i)
		frame := rotate(base.image, wheel)
		x, y := point(ballRadius, ballAngle)
		disc(frame, x+1.5, y+2, ballSize, color.RGBA{0, 0, 0, 90}, color.RGBA{0, 0, 0, 90}) // shadow
		disc(frame, x, y, ballSize, color.RGBA{0xf7, 0xf7, 0xf2, 0xff}, color.RGBA{0x9a, 0x9a, 0x90, 0xff})

		delay := frameDelay
		if i == frames-1 {
			delay = 250 // linger on where it stopped
		}
		anim.Image = append(anim.Image, base.quantize(frame))
		anim.Delay = append(anim.Delay, delay)
	}
	var out bytes.Buffer
	if err := gif.EncodeAll(&out, anim); err != nil {
		return nil, nil, err
	}

	still, err = render.PNG(ctx, FrameSVG(result, frames-1, true))
	return out.Bytes(), still, err
}

// wheelImage is the wheel at rest, with a GIF palette and a lookup table from
// 15-bit RGB to the nearest palette entry.
type wheelImage struct {
	image   *image.RGBA
	palette color.Palette
	nearest []uint8
}

var (
	wheelMu    sync.Mutex
	wheelCache *wheelImage
)

func cachedWheel(ctx context.Context) (*wheelImage, error) {
	wheelMu.Lock()
	defer wheelMu.Unlock()
	if wheelCache != nil {
		return wheelCache, nil
	}

	pngData, err := render.PNG(ctx, wheelSVG(0, -1))
	if err != nil {
		return nil, err
	}
	decoded, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, image.Point{}, draw.Src)

	// Let ImageMagick pick the wheel's 248 best colours, then add the ball's
	gifData, err := render.Magick(ctx, wheelSVG(0, -1), "svg:-", "-colors", "248", "gif:-")
	if err != nil {
		return nil, err
	}
	quantized, err := gif.Decode(bytes.NewReader(gifData))
	if err != nil {
		return nil, err
	}
	// GIF pads its colour table to 256 with repeats; keep each colour once
	var palette color.Palette
	for _, c := range quantized.(*image.Paletted).Palette {
		if len(palette) < 253 && !slices.Contains(palette, c) {
			palette = append(palette, c)
		}
	}
	palette = append(palette, color.RGBA{0xf7, 0xf7, 0xf2, 0xff}, color.RGBA{0x9a, 0x9a, 0x90, 0xff}, color.RGBA{0xd0, 0xd0, 0xc8, 0xff})

	nearest := make([]uint8, 1<<15)
	for key := range nearest {
		c := color.RGBA{uint8(key>>10) << 3, uint8(key>>5&31) << 3, uint8(key&31) << 3, 0xff}
		nearest[key] = uint8(palette.Index(c))
	}
	wheelCache = &wheelImage{image: img, palette: palette, nearest: nearest}
	return wheelCache, nil
}

func (w *wheelImage) quantize(img *image.RGBA) *image.Paletted {
	out := image.NewPaletted(img.Bounds(), w.palette)
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b := img.Pix[i], img.Pix[i+1], img.Pix[i+2]
		out.Pix[i/4] = w.nearest[int(r>>3)<<10|int(g>>3)<<5|int(b>>3)]
	}
	return out
}

// rotate turns img clockwise by degrees around its center, smoothing with
// bilinear sampling. Corners outside the picture take the top-left colour.
func rotate(img *image.RGBA, degrees float64) *image.RGBA {
	out := image.NewRGBA(img.Bounds())
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	c := float64(size) / 2
	bg := img.RGBAAt(0, 0)
	for y := range size {
		for x := range size {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			// The source pixel is this one turned back the other way
			sx, sy := c+dx*cos+dy*sin-0.5, c-dx*sin+dy*cos-0.5
			out.SetRGBA(x, y, bilinear(img, sx, sy, bg))
		}
	}
	return out
}

func bilinear(img *image.RGBA, x, y float64, bg color.RGBA) color.RGBA {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(px, py int) color.RGBA {
		if px < 0 || py < 0 || px >= size || py >= size {
			return bg
		}
		return img.RGBAAt(px, py)
	}
	c00, c10, c01, c11 := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
	mix := func(a, b, c, d uint8) uint8 {
		top := float64(a)*(1-fx) + float64(b)*fx
		bottom := float64(c)*(1-fx) + float64(d)*fx
		return uint8(top*(1-fy) + bottom*fy + 0.5)
	}
	return color.RGBA{mix(c00.R, c10.R, c01.R, c11.R), mix(c00.G, c10.G, c01.G, c11.G), mix(c00.B, c10.B, c01.B, c11.B), 0xff}
}

// disc paints an anti-aliased filled circle with a thin edge colour.
func disc(img *image.RGBA, cx, cy, r float64, fill, edge color.RGBA) {
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			coverage := math.Min(math.Max(r+0.5-d, 0), 1) * float64(fill.A) / 255
			if coverage == 0 || x < 0 || y < 0 || x >= size || y >= size {
				continue
			}
			paint := fill
			if d > r-1.2 {
				paint = edge
			}
			under := img.RGBAAt(x, y)
			blend := func(top, bottom uint8) uint8 {
				return uint8(float64(top)*coverage + float64(bottom)*(1-coverage) + 0.5)
			}
			img.SetRGBA(x, y, color.RGBA{blend(paint.R, under.R), blend(paint.G, under.G), blend(paint.B, under.B), 0xff})
		}
	}
}

// Positions returns the wheel's rotation and the ball's angle and distance
// from the center in frame i of a spin landing on result. Angles are degrees
// clockwise from 12 o'clock.
//
// The wheel turns clockwise and slows to a stop; the ball circles the other
// way, drops in, and from 85% of the way through rides in its pocket.
func Positions(result, i int) (wheel, ballAngle, ballRadius float64) {
	t := float64(i) / float64(frames-1)
	pocket := float64(slices.Index(WheelOrder[:], result)) * step

	const wheelEnd, wheelSpin = 0.0, 450.0
	wheelAt := func(t float64) float64 { return wheelEnd - wheelSpin*(1-t)*(1-t) }
	wheel = wheelAt(t)

	const lock, drop, ballSpin = 0.85, 0.62, 1080.0
	if t >= lock {
		return wheel, wheel + pocket, ballRest
	}
	// Slow to a stop exactly where the pocket is at the moment it locks
	ballAngle = wheelAt(lock) + pocket + ballSpin*math.Pow(1-t/lock, 2)
	ballRadius = ballTrack
	if t > drop {
		s := (t - drop) / (lock - drop)
		ease := s * s * (3 - 2*s)
		bounce := 6 * math.Sin(s*2*math.Pi) * (1 - s)
		ballRadius = ballTrack - (ballTrack-ballRest)*ease + bounce
	}
	return wheel, ballAngle, ballRadius
}

// FrameSVG draws frame i of a spin landing on result. final adds the result
// in the middle of the wheel.
func FrameSVG(result, i int, final bool) string {
	wheel, ballAngle, ballRadius := Positions(result, i)
	badge := -1
	if final {
		badge = result
	}
	svg := wheelSVG(wheel, badge)

	bx, by := point(ballRadius, ballAngle)
	ball := fmt.Sprintf(`<circle cx="%.2f" cy="%.2f" r="%.1f" fill="#000" opacity="0.35"/>`, bx+1.5, by+2, ballSize) +
		fmt.Sprintf(`<circle cx="%.2f" cy="%.2f" r="%.1f" fill="#f7f7f2" stroke="#9a9a90" stroke-width="1"/>`, bx, by, ballSize)
	return strings.Replace(svg, `</svg>`, ball+`</svg>`, 1)
}

var pocketFill = map[string]string{"green": "#1f7a3a", "red": "#b3262e", "black": "#1c1c1c"}

// wheelSVG draws the wheel turned by rotation degrees, without the ball.
// badge, if not -1, is shown as the result in the middle.
func wheelSVG(rotation float64, badge int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`, size, size, size, size)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#13181b"/>`, size, size)
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="196" fill="#5b3a1f"/><circle cx="%d" cy="%d" r="188" fill="#2b211a"/>`, center, center, center, center)

	fmt.Fprintf(&b, `<g transform="rotate(%.2f %d %d)">`, rotation, center, center)
	for idx, n := range WheelOrder {
		a := float64(idx) * step
		x1, y1 := point(pocketOuter, a-step/2)
		x2, y2 := point(pocketOuter, a+step/2)
		x3, y3 := point(pocketInner, a+step/2)
		x4, y4 := point(pocketInner, a-step/2)
		fmt.Fprintf(&b, `<path d="M%.2f %.2f A%.0f %.0f 0 0 1 %.2f %.2f L%.2f %.2f A%.0f %.0f 0 0 0 %.2f %.2f Z" fill="%s" stroke="#c9a227" stroke-width="1"/>`,
			x1, y1, pocketOuter, pocketOuter, x2, y2, x3, y3, pocketInner, pocketInner, x4, y4, pocketFill[Color(n)])
		fmt.Fprintf(&b, `<text transform="rotate(%.2f %d %d)" x="%d" y="%.1f" text-anchor="middle" font-family="DejaVu Sans" font-size="13" font-weight="700" fill="#f4efe4">%d</text>`,
			a, center, center, center, center-numberAt+5, n)
	}
	// Wooden cone with brass spokes
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="%.0f" fill="#8a5a2b" stroke="#c9a227" stroke-width="2"/>`, center, center, pocketInner)
	for spoke := range 4 {
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="6" height="%d" rx="3" fill="#c9a227" transform="rotate(%d %d %d)"/>`,
			center-3, center-int(pocketInner)+18, 2*int(pocketInner)-36, spoke*45, center, center)
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="22" fill="#c9a227" stroke="#7a5f12" stroke-width="2"/>`, center, center)

	if badge >= 0 {
		fmt.Fprintf(&b, `<circle cx="%d" cy="%d" r="46" fill="%s" stroke="#c9a227" stroke-width="4"/>`, center, center, pocketFill[Color(badge)])
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="middle" font-family="DejaVu Sans" font-size="40" font-weight="700" fill="#f4efe4">%d</text>`, center, center+14, badge)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// point is the position at radius r and angle a (degrees clockwise from 12 o'clock).
func point(r, a float64) (float64, float64) {
	rad := a * math.Pi / 180
	return center + r*math.Sin(rad), center - r*math.Cos(rad)
}
