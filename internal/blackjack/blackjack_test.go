package blackjack

import (
	"bytes"
	"context"
	"os/exec"
	"slices"
	"testing"
)

// stacked returns a shoe that deals these ranks in order (player, dealer,
// player, dealer, then hits).
func stacked(ranks ...int) []Card {
	shoe := make([]Card, len(ranks))
	for i, rank := range ranks {
		shoe[i] = Card{Rank: rank}
	}
	slices.Reverse(shoe) // cards are drawn from the end
	return shoe
}

func TestHandValueCountsAcesAsOneOrEleven(t *testing.T) {
	for _, c := range []struct {
		ranks []int
		total int
		soft  bool
	}{
		{[]int{1, 13}, 21, true}, {[]int{1, 1, 9}, 21, true}, {[]int{1, 13, 5}, 16, false}, {[]int{1, 6}, 17, true}, {[]int{10, 6}, 16, false},
	} {
		var hand Hand
		for _, r := range c.ranks {
			hand = append(hand, Card{Rank: r})
		}
		if total, soft := hand.Value(); total != c.total || soft != c.soft {
			t.Errorf("%v = %d (soft %v), want %d (soft %v)", c.ranks, total, soft, c.total, c.soft)
		}
	}
}

func TestHandsPlayOutAndPay(t *testing.T) {
	for _, c := range []struct {
		name    string
		shoe    []int
		play    func(*Game)
		outcome Outcome
		payout  int64
	}{
		{"natural pays 3:2", []int{1, 9, 13, 7}, func(*Game) {}, PlayerBlackjack, 250},
		{"dealer stands on soft 17", []int{10, 1, 7, 6}, (*Game).Stand, Push, 100},
		{"dealer draws and busts", []int{10, 10, 8, 6, 9}, (*Game).Stand, DealerBusts, 200},
		{"double takes one card", []int{5, 10, 6, 7, 10}, (*Game).Double, PlayerWins, 400},
		{"hit and bust", []int{10, 9, 6, 7, 13}, (*Game).Hit, PlayerBusts, 0},
	} {
		g := Deal(stacked(c.shoe...), 100)
		c.play(g)
		if g.Outcome != c.outcome || g.Payout() != c.payout {
			t.Errorf("%s: outcome %v paying %d, want %v paying %d", c.name, g.Outcome, g.Payout(), c.outcome, c.payout)
		}
	}

	g := Deal(stacked(5, 10, 6, 7, 10, 2, 3), 100)
	g.Double()
	if len(g.Player) != 3 || g.Bet != 200 {
		t.Fatalf("after doubling: %d player cards, bet %d; want 3 cards, bet 200", len(g.Player), g.Bet)
	}
}

func TestRenderPNGWhenImageMagickIsAvailable(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick is not installed")
	}
	png, err := Deal(stacked(10, 9, 6, 7), 100).RenderPNG(context.Background(), "Tester <&>")
	if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("RenderPNG() = %d bytes, %v; want a PNG", len(png), err)
	}
}
