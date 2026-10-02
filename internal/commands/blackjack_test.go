package commands

import (
	"path/filepath"
	"testing"

	"discord_gobot/internal/blackjack"

	"github.com/disgoorg/disgo/discord"
)

func TestBlackjackOneHandPerPlayerAndRefundOnShutdown(t *testing.T) {
	casino := NewCasino(filepath.Join(t.TempDir(), "casino.json"))
	tables := NewBlackjack(casino)
	player := discord.User{ID: 7}

	hand := &blackjackTable{id: "a", guildID: 1, player: player}
	if !tables.claim(hand) {
		t.Fatal("first hand was refused")
	}
	if tables.claim(&blackjackTable{id: "b", guildID: 1, player: player}) {
		t.Fatal("a second hand in the same server was allowed")
	}
	if !tables.claim(&blackjackTable{id: "c", guildID: 2, player: player}) {
		t.Fatal("a hand in another server was refused")
	}

	casino.take(1, 7, 100)
	// Dealt from the end: player 10, dealer 9, player 6, dealer 7 (no blackjack)
	hand.game = blackjack.Deal([]blackjack.Card{{Rank: 7}, {Rank: 6}, {Rank: 9}, {Rank: 10}}, 100)
	tables.Shutdown()
	if got := casino.coins(1, 7); got != startingCoins {
		t.Fatalf("balance after shutdown = %d, want the bet refunded (%d)", got, startingCoins)
	}
}
