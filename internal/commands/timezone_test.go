package commands

import (
	"testing"
	"time"
)

func TestFindTimesReadsClockTimesInTheWritersZone(t *testing.T) {
	zone := time.FixedZone("BST", 3600)
	sent := time.Date(2026, 10, 2, 12, 0, 0, 0, zone)

	found := findTimes("on at 9pm, 21:30 latest. back by 7 AM? score was 3:2, 9pm again", sent)
	want := []foundTime{
		{"9pm", time.Date(2026, 10, 2, 21, 0, 0, 0, zone)},
		{"21:30", time.Date(2026, 10, 2, 21, 30, 0, 0, zone)},
		{"7 AM", time.Date(2026, 10, 3, 7, 0, 0, 0, zone)}, // already past at noon: tomorrow
	}
	if len(found) != len(want) {
		t.Fatalf("findTimes found %v, want %v", found, want)
	}
	for i := range want {
		if found[i].text != want[i].text || !found[i].at.Equal(want[i].at) {
			t.Errorf("time %d = %s at %v, want %s at %v", i, found[i].text, found[i].at, want[i].text, want[i].at)
		}
	}

	if found := findTimes("no times in here, just 1:2 and 123:45", sent); len(found) != 0 {
		t.Fatalf("findTimes found %v in text without times", found)
	}
}
