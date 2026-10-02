package commands

import (
	"path/filepath"
	"testing"
)

func TestSlotsKeepASmallHouseEdge(t *testing.T) {
	total := 0
	for _, s := range slotSymbols {
		total += s.weight
	}
	// Exact average payout per coin over every possible spin
	var paid float64
	for _, a := range slotSymbols {
		for _, b := range slotSymbols {
			for _, c := range slotSymbols {
				chance := float64(a.weight*b.weight*c.weight) / float64(total*total*total)
				paid += chance * float64(slotMultiplier([3]string{a.symbol, b.symbol, c.symbol}))
			}
		}
	}
	if paid < 0.90 || paid >= 1.0 {
		t.Fatalf("slots pay back %.3f per coin on average, want a small house edge (0.90 to 1.00)", paid)
	}

	for reels, want := range map[[3]string]int64{
		{"7️⃣", "7️⃣", "7️⃣"}: 500,
		{"🍒", "🍋", "🍒"}:       2,
		{"🍋", "🍇", "🍇"}:       1,
		{"🍒", "🍋", "🍇"}:       0,
	} {
		if got := slotMultiplier(reels); got != want {
			t.Errorf("slotMultiplier(%v) = %d, want %d", reels, got, want)
		}
	}
}

func TestCasinoBetsNeedTheCoinsAndAreSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "casino.json")
	casino := NewCasino(path)

	if _, problem := casino.play(1, 2, startingCoins+1, func() int64 { return 0 }); problem == "" {
		t.Fatal("a bet larger than the balance was accepted")
	}
	if balance, problem := casino.play(1, 2, 100, func() int64 { return 200 }); problem != "" || balance != startingCoins+100 {
		t.Fatalf("winning 100 left balance %d (%q), want %d", balance, problem, startingCoins+100)
	}
	if balance, _ := casino.play(1, 2, 300, func() int64 { return 0 }); balance != startingCoins-200 {
		t.Fatalf("losing 300 left balance %d, want %d", balance, startingCoins-200)
	}

	reloaded := NewCasino(path)
	if got := reloaded.accounts[1][2].Coins; got != startingCoins-200 {
		t.Fatalf("balance after restart = %d, want %d", got, startingCoins-200)
	}
	if reloaded.accountLocked(9, 2).Coins != startingCoins {
		t.Fatal("balances are not separate per server")
	}
}
