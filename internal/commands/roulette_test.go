package commands

import "testing"

func TestRouletteBetsPayFairOddsExceptTheZero(t *testing.T) {
	bets := []struct {
		on     string
		number int
	}{{"red", 0}, {"black", 0}, {"odd", 0}, {"even", 0}, {"low", 0}, {"high", 0}, {"dozen1", 0}, {"dozen2", 0}, {"dozen3", 0}}
	for n := range 37 {
		bets = append(bets, struct {
			on     string
			number int
		}{"number", n})
	}

	// Over all 37 results every bet returns 36 per coin: fair odds on 36
	// numbers, with the zero as the house's 1-in-37 edge.
	for _, bet := range bets {
		var paid int64
		for result := range 37 {
			paid += rouletteMultiplier(bet.on, bet.number, result)
		}
		if paid != 36 {
			t.Errorf("%s %d pays %d over all results, want 36", bet.on, bet.number, paid)
		}
	}

	for _, c := range []struct {
		on     string
		number int
		result int
		want   int64
	}{
		{"red", 0, 1, 2}, {"black", 0, 1, 0}, {"dozen3", 0, 25, 3}, {"low", 0, 0, 0}, {"number", 0, 0, 36}, {"number", 17, 17, 36},
	} {
		if got := rouletteMultiplier(c.on, c.number, c.result); got != c.want {
			t.Errorf("rouletteMultiplier(%s %d, landed %d) = %d, want %d", c.on, c.number, c.result, got, c.want)
		}
	}
}
