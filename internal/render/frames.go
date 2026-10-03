package render

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"slices"
)

// RGBA draws an SVG document as an in-memory image, for building animation
// frames in Go.
func RGBA(ctx context.Context, svg string) (*image.RGBA, error) {
	data, err := PNG(ctx, svg)
	if err != nil {
		return nil, err
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(decoded.Bounds())
	draw.Draw(img, img.Bounds(), decoded, image.Point{}, draw.Src)
	return img, nil
}

// Palette is a GIF colour table with a lookup from 15-bit RGB to its nearest
// entry, so frames convert to GIF colours quickly.
type Palette struct {
	Colors  color.Palette
	nearest []uint8
}

// NewPalette takes the best colours of an SVG drawing (picked by
// ImageMagick) plus any extra colours, up to GIF's 256.
func NewPalette(ctx context.Context, svg string, extra ...color.Color) (*Palette, error) {
	data, err := Magick(ctx, svg, "svg:-", "-colors", "248", "gif:-")
	if err != nil {
		return nil, err
	}
	quantized, err := gif.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// GIF pads its colour table to 256 with repeats; keep each colour once
	var colors color.Palette
	for _, c := range quantized.(*image.Paletted).Palette {
		if len(colors) < 256-len(extra) && !slices.Contains(colors, c) {
			colors = append(colors, c)
		}
	}
	colors = append(colors, extra...)

	nearest := make([]uint8, 1<<15)
	for key := range nearest {
		c := color.RGBA{uint8(key>>10) << 3, uint8(key>>5&31) << 3, uint8(key&31) << 3, 0xff}
		nearest[key] = uint8(colors.Index(c))
	}
	return &Palette{Colors: colors, nearest: nearest}, nil
}

// Quantize converts the part of img inside r to GIF colours. A frame
// smaller than the picture only replaces that area when played.
func (p *Palette) Quantize(img *image.RGBA, r image.Rectangle) *image.Paletted {
	out := image.NewPaletted(r, p.Colors)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.Pix[img.PixOffset(x, y):]
			out.Pix[out.PixOffset(x, y)] = p.nearest[int(c[0]>>3)<<10|int(c[1]>>3)<<5|int(c[2]>>3)]
		}
	}
	return out
}
