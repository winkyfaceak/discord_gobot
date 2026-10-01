package commands

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.FixedZone("BST", 3600))
	tests := []struct {
		in   string
		want time.Time
	}{
		{"20m", now.Add(20 * time.Minute)},
		{"20 minutes", now.Add(20 * time.Minute)},
		{"1h30m", now.Add(90 * time.Minute)},
		{"1 hour and 30 mins", now.Add(90 * time.Minute)},
		{"2 days", now.Add(48 * time.Hour)},
		{"2d 3h", now.Add(51 * time.Hour)},
		{"18:30", time.Date(2026, 10, 1, 18, 30, 0, 0, now.Location())},
		{"09:00", time.Date(2026, 10, 2, 9, 0, 0, 0, now.Location())}, // already passed today
		{"6pm", time.Date(2026, 10, 1, 18, 0, 0, 0, now.Location())},
		{"6:30 PM", time.Date(2026, 10, 1, 18, 30, 0, 0, now.Location())},
	}
	for _, test := range tests {
		got, ok := parseWhen(test.in, now)
		if !ok || !got.Equal(test.want) {
			t.Errorf("parseWhen(%q) = %v, %v; want %v", test.in, got, ok, test.want)
		}
	}

	for _, in := range []string{"", "soon", "20", "0m", "-5m", "xd"} {
		if got, ok := parseWhen(in, now); ok {
			t.Errorf("parseWhen(%q) = %v, want rejected", in, got)
		}
	}
}

func TestRemindersSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.json")
	first := NewRemind(path)
	rem := reminder{ID: "abc", ChannelID: 1, CreatorID: 2, TargetID: 3, Message: "raid", Due: time.Now().Add(time.Hour).Round(0)}
	first.reminders[rem.ID] = rem
	if err := first.saveLocked(); err != nil {
		t.Fatal(err)
	}

	saved, err := loadReminders(path)
	if err != nil || len(saved) != 1 || saved[0] != rem {
		t.Fatalf("loadReminders() = %+v, %v; want [%+v]", saved, err, rem)
	}
	if missing, err := loadReminders(filepath.Join(t.TempDir(), "none.json")); err != nil || missing != nil {
		t.Fatalf("loadReminders(missing) = %v, %v; want nothing", missing, err)
	}
}
