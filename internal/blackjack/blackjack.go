// Package blackjack plays one hand of blackjack against a dealer and draws
// the table. Rules: six decks, the dealer stands on all 17s, blackjack pays
// 3:2, and the player may double on the first two cards. No splitting,
// insurance or surrender.
package blackjack

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"

	"discord_gobot/internal/render"
)

// Card has a rank from 1 (ace) to 13 (king) and a suit from 0 to 3.
type Card struct {
	Rank int
	Suit int
}

var suits = [4]string{"♠", "♥", "♦", "♣"}

func (c Card) String() string {
	return [14]string{"", "A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}[c.Rank] + suits[c.Suit]
}

// NewShoe returns decks shuffled together.
func NewShoe(decks int) []Card {
	var shoe []Card
	for range decks {
		for suit := range 4 {
			for rank := 1; rank <= 13; rank++ {
				shoe = append(shoe, Card{rank, suit})
			}
		}
	}
	rand.Shuffle(len(shoe), func(i, j int) { shoe[i], shoe[j] = shoe[j], shoe[i] })
	return shoe
}

type Hand []Card

// Value is the best total of the hand, and whether an ace is counting as 11.
func (h Hand) Value() (total int, soft bool) {
	aces := 0
	for _, c := range h {
		total += min(c.Rank, 10)
		if c.Rank == 1 {
			aces++
		}
	}
	if aces > 0 && total+10 <= 21 {
		return total + 10, true
	}
	return total, false
}

func (h Hand) blackjack() bool {
	total, _ := h.Value()
	return len(h) == 2 && total == 21
}

type Outcome int

const (
	Playing Outcome = iota
	PlayerBlackjack
	PlayerWins
	DealerBusts
	Push
	PlayerBusts
	DealerWins
	DealerBlackjack
)

// Game is one hand. Bet doubles when the player doubles down.
type Game struct {
	Player, Dealer Hand
	Bet            int64
	Outcome        Outcome
	shoe           []Card
}

// Deal starts a hand from the shoe. A blackjack on either side ends it at once.
func Deal(shoe []Card, bet int64) *Game {
	g := &Game{Bet: bet, shoe: shoe}
	g.Player = Hand{g.draw()}
	g.Dealer = Hand{g.draw()}
	g.Player = append(g.Player, g.draw())
	g.Dealer = append(g.Dealer, g.draw())

	switch {
	case g.Player.blackjack() && g.Dealer.blackjack():
		g.Outcome = Push
	case g.Player.blackjack():
		g.Outcome = PlayerBlackjack
	case g.Dealer.blackjack():
		g.Outcome = DealerBlackjack
	}
	return g
}

func (g *Game) draw() Card {
	card := g.shoe[len(g.shoe)-1]
	g.shoe = g.shoe[:len(g.shoe)-1]
	return card
}

// Done reports whether the hand is over.
func (g *Game) Done() bool { return g.Outcome != Playing }

// CanDouble reports whether the player may still double down.
func (g *Game) CanDouble() bool { return !g.Done() && len(g.Player) == 2 }

// Hit gives the player another card.
func (g *Game) Hit() {
	if g.Done() {
		return
	}
	g.Player = append(g.Player, g.draw())
	if total, _ := g.Player.Value(); total > 21 {
		g.Outcome = PlayerBusts
	}
}

// Double doubles the bet, takes exactly one more card and stands. The caller
// collects the extra bet first.
func (g *Game) Double() {
	if !g.CanDouble() {
		return
	}
	g.Bet *= 2
	g.Hit()
	g.Stand()
}

// Stand ends the player's turn: the dealer draws to 17, then the hand is settled.
func (g *Game) Stand() {
	if g.Done() {
		return
	}
	for {
		if total, _ := g.Dealer.Value(); total >= 17 {
			break
		}
		g.Dealer = append(g.Dealer, g.draw())
	}

	player, _ := g.Player.Value()
	dealer, _ := g.Dealer.Value()
	switch {
	case dealer > 21:
		g.Outcome = DealerBusts
	case player > dealer:
		g.Outcome = PlayerWins
	case player == dealer:
		g.Outcome = Push
	default:
		g.Outcome = DealerWins
	}
}

// Payout is what the player gets back: the bet plus 3:2 for blackjack, the
// bet doubled for a win, the bet itself for a push, otherwise nothing.
func (g *Game) Payout() int64 {
	switch g.Outcome {
	case PlayerBlackjack:
		return g.Bet + g.Bet*3/2
	case PlayerWins, DealerBusts:
		return 2 * g.Bet
	case Push:
		return g.Bet
	default:
		return 0
	}
}

// Summary describes the outcome in a few words.
func (g *Game) Summary() string {
	return map[Outcome]string{
		PlayerBlackjack: "Blackjack!",
		PlayerWins:      "You win!",
		DealerBusts:     "Dealer busts, you win!",
		Push:            "Push: it's a tie.",
		PlayerBusts:     "Bust!",
		DealerWins:      "Dealer wins.",
		DealerBlackjack: "Dealer has blackjack.",
	}[g.Outcome]
}

// RenderPNG draws the table. The dealer's second card stays face down until
// the hand is over.
func (g *Game) RenderPNG(ctx context.Context, playerName string) ([]byte, error) {
	return render.PNG(ctx, g.SVG(playerName))
}

const (
	width      = 720
	height     = 440
	cardWidth  = 86
	cardHeight = 120
)

// SVG draws the table (see RenderPNG).
func (g *Game) SVG(playerName string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg width="%d" height="%d" viewBox="0 0 %d %d" xmlns="http://www.w3.org/2000/svg">`, width, height, width, height)
	b.WriteString(`<defs><radialGradient id="felt" cx="50%" cy="40%" r="75%"><stop offset="0" stop-color="#11734a"/><stop offset="1" stop-color="#073d27"/></radialGradient>`)
	b.WriteString(`<pattern id="back" width="10" height="10" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="10" height="10" fill="#1d3e8a"/><rect width="4" height="10" fill="#2a54b0"/></pattern></defs>`)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" rx="18" fill="url(#felt)" stroke="#5b3a1f" stroke-width="10"/>`, width, height)
	fmt.Fprintf(&b, `<text x="%d" y="40" text-anchor="end" font-family="DejaVu Sans" font-size="18" font-weight="700" fill="#f2d16b">BET %d</text>`, width-36, g.Bet)

	dealerLabel := "?"
	if g.Done() {
		dealerLabel = g.Dealer.Label()
	}
	writeHand(&b, "DEALER", dealerLabel, g.Dealer, 40, !g.Done())
	writeHand(&b, strings.ToUpper(playerName), g.Player.Label(), g.Player, 228, false)

	if g.Done() {
		color := "#b3262e"
		if g.Payout() > g.Bet {
			color = "#1f7a3a"
		} else if g.Outcome == Push {
			color = "#7a6a1f"
		}
		fmt.Fprintf(&b, `<rect x="370" y="190" width="320" height="56" rx="10" fill="%s" stroke="#f2d16b" stroke-width="2"/>`, color)
		fmt.Fprintf(&b, `<text x="530" y="225" text-anchor="middle" font-family="DejaVu Sans" font-size="18" font-weight="700" fill="#fff">%s</text>`, render.Escape(strings.ToUpper(g.Summary())))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// Label is the total, written "soft 17" when an ace counts as 11.
func (h Hand) Label() string {
	total, soft := h.Value()
	if soft && total < 21 {
		return fmt.Sprintf("soft %d", total)
	}
	return fmt.Sprint(total)
}

func writeHand(b *strings.Builder, name, total string, hand Hand, top int, hideSecond bool) {
	// The total follows the name in the same line, however long the name is
	fmt.Fprintf(b, `<text x="40" y="%d" font-family="DejaVu Sans" font-size="16" font-weight="700" letter-spacing="2" fill="#d8efe2">%s<tspan dx="14" letter-spacing="0" font-weight="400" fill="#f2d16b">%s</tspan></text>`,
		top+18, render.Escape(name), total)

	gap := 96
	if len(hand) > 1 {
		gap = min(gap, (360-cardWidth)/(len(hand)-1))
	}
	for i, card := range hand {
		x, y := 40+i*gap, top+30
		if hideSecond && i == 1 {
			fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="%d" rx="8" fill="url(#back)" stroke="#f4efe4" stroke-width="4"/>`, x, y, cardWidth, cardHeight)
			continue
		}
		color := "#1b1b1b"
		if card.Suit == 1 || card.Suit == 2 {
			color = "#c0392b"
		}
		rank := strings.TrimSuffix(card.String(), suits[card.Suit])
		fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="%d" rx="8" fill="#fbfaf6" stroke="#b9b4a8" stroke-width="1.5"/>`, x, y, cardWidth, cardHeight)
		fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="20" font-weight="700" fill="%s">%s</text>`, x+8, y+26, color, rank)
		fmt.Fprintf(b, `<text x="%d" y="%d" font-family="DejaVu Sans" font-size="16" fill="%s">%s</text>`, x+9, y+46, color, suits[card.Suit])
		fmt.Fprintf(b, `<text x="%d" y="%d" text-anchor="middle" font-family="DejaVu Sans" font-size="44" fill="%s">%s</text>`, x+cardWidth/2, y+cardHeight/2+20, color, suits[card.Suit])
	}
}
