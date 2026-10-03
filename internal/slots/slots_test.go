package slots

import (
	"bytes"
	"context"
	"image/gif"
	"os/exec"
	"testing"
)

func TestSlotsKeepASmallHouseEdge(t *testing.T) {
	total := 0
	for _, s := range Symbols {
		total += s.Weight
	}
	// Exact average payout per coin over every possible spin
	var paid float64
	for a := range Symbols {
		for b := range Symbols {
			for c := range Symbols {
				chance := float64(Symbols[a].Weight*Symbols[b].Weight*Symbols[c].Weight) / float64(total*total*total)
				paid += chance * float64(Multiplier([3]int{a, b, c}))
			}
		}
	}
	if paid < 0.90 || paid >= 1.0 {
		t.Fatalf("slots pay back %.3f per coin on average, want a small house edge (0.90 to 1.00)", paid)
	}

	seven, lemon, grapes := len(Symbols)-1, 1, 3
	for reels, want := range map[[3]int]int64{
		{seven, seven, seven}:   500,
		{cherry, lemon, cherry}: 2,
		{lemon, grapes, grapes}: 1,
		{cherry, lemon, grapes}: 0,
	} {
		if got := Multiplier(reels); got != want {
			t.Errorf("Multiplier(%v) = %d, want %d", reels, got, want)
		}
	}
}

func TestEverySymbolStopsOnThePayline(t *testing.T) {
	middle := (windowHeight - 2*frameBorder) / 2
	for symbol := range Symbols {
		offset := restingOffset(symbol)
		if offset < 0 || offset >= stripHeight() {
			t.Fatalf("symbol %d rests at offset %d, outside the strip", symbol, offset)
		}
		// The window's middle row is the centre of the symbol's cell
		if centre := (offset + middle) % stripHeight(); centre != cell/2+symbol*cell {
			t.Errorf("symbol %d: payline shows strip row %d, want %d", symbol, centre, cell/2+symbol*cell)
		}
	}
}

func TestRenderSpinWhenImageMagickIsAvailable(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick is not installed")
	}
	spin, still, err := RenderSpin(context.Background(), [3]int{0, 3, 6})
	if err != nil {
		t.Fatalf("RenderSpin() error = %v", err)
	}
	anim, err := gif.DecodeAll(bytes.NewReader(spin))
	if err != nil || len(anim.Image) != frames {
		t.Fatalf("spin GIF has %d frames (err %v), want %d", len(anim.Image), err, frames)
	}
	if !bytes.HasPrefix(still, []byte("\x89PNG")) {
		t.Fatal("RenderSpin() still is not a PNG")
	}
}
