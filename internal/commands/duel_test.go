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
