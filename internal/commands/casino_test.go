package commands

import (
	"path/filepath"
	"testing"
)

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
