package commands

import "testing"

func TestRockPaperScissors(t *testing.T) {
	const rock, paper, scissors = 0, 1, 2
	for _, win := range [][2]int{{paper, rock}, {scissors, paper}, {rock, scissors}} {
		if !rpsBeats(win[0], win[1]) || rpsBeats(win[1], win[0]) {
			t.Errorf("%s should beat %s, not the other way round", rpsHands[win[0]], rpsHands[win[1]])
		}
	}
	for hand := range rpsHands {
		if rpsBeats(hand, hand) {
			t.Errorf("%s beats itself", rpsHands[hand])
		}
	}
}

func TestConnect4FindsFourInARow(t *testing.T) {
	// play drops pieces into columns in turn (red first) and reports whether
	// the last one won.
	play := func(cols ...int) bool {
		var b c4Board
		var row, col int
		for i, c := range cols {
			row, col = b.drop(c, i%2+1), c
		}
		return b.wins(row, col)
	}
	for name, cols := range map[string][]int{
		"across":   {0, 0, 1, 1, 2, 2, 3},
		"down":     {0, 1, 0, 1, 0, 1, 0},
		"up-right": {0, 1, 1, 2, 2, 3, 2, 3, 3, 6, 3},
		"up-left":  {6, 5, 5, 4, 4, 3, 4, 3, 3, 0, 3},
		"middle":   {0, 0, 1, 1, 3, 3, 2}, // the winning piece fills the gap
	} {
		if !play(cols...) {
			t.Errorf("%s: four in a row wasn't a win", name)
		}
	}
	if play(0, 0, 1, 1, 2) {
		t.Error("three in a row was a win")
	}

	var b c4Board
	for range c4Rows {
		b.drop(0, 1)
	}
	if b.drop(0, 2) != -1 {
		t.Error("a piece fit in a full column")
	}
}
