package roulette

import (
	"bytes"
	"context"
	"image/gif"
	"math"
	"os/exec"
	"slices"
	"testing"
)

func TestWheelIsAEuropeanWheel(t *testing.T) {
	sorted := slices.Clone(WheelOrder[:])
	slices.Sort(sorted)
	for n, got := range sorted {
		if got != n {
			t.Fatalf("wheel numbers = %v, want each of 0 to 36 once", sorted)
		}
	}
	// Going round from 0, colours alternate red, black, red, ...
	for i, n := range WheelOrder[1:] {
		want := "red"
		if i%2 == 1 {
			want = "black"
		}
		if Color(n) != want {
			t.Fatalf("pocket %d (number %d) is %s, want %s", i+1, n, Color(n), want)
		}
	}
}

func TestBallEndsInTheWinningPocket(t *testing.T) {
	for _, result := range WheelOrder {
		wheel, ball, radius := Positions(result, frames-1)
		pocket := float64(slices.Index(WheelOrder[:], result)) * step
		if math.Abs(ball-(wheel+pocket)) > 1e-9 || radius != ballRest {
			t.Fatalf("spin to %d ends with the ball at %.2f° r=%.1f, want the pocket at %.2f° r=%.1f", result, ball, radius, wheel+pocket, ballRest)
		}
	}
}

func TestRenderSpinWhenImageMagickIsAvailable(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick is not installed")
	}
	spin, still, err := RenderSpin(context.Background(), 17)
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
