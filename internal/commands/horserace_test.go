package commands

import (
	"math/rand/v2"
	"testing"

	"github.com/disgoorg/snowflake/v2"
)

func TestRacesFinishAndEveryHorseHasTheSameChance(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	const races = 20000
	var wins [raceHorses]int
	for range races {
		ticks, winner := runRace(rng.IntN)
		if last := ticks[len(ticks)-1]; last[winner] < raceFinish {
			t.Fatalf("winner %d finished at %d, short of the line", winner, last[winner])
		}
		wins[winner]++
	}
	for horse, n := range wins {
		if share := float64(n) / races; share < 0.18 || share > 0.22 {
			t.Errorf("%s won %.1f%% of races, want about 20%%", raceHorseNames[horse], share*100)
		}
	}
}

func TestRaceWinningsSplitThePot(t *testing.T) {
	bets := map[snowflake.ID]int{1: 0, 2: 0, 3: 4, 4: 2}
	won := raceWinnings(bets, 0, 400)
	if len(won) != 2 || won[1] != 200 || won[2] != 200 {
		t.Fatalf("winnings = %v, want riders 1 and 2 to get 200 each", won)
	}
	if won := raceWinnings(bets, 1, 400); len(won) != 0 {
		t.Fatalf("winnings with no backers = %v, want none", won)
	}
}
