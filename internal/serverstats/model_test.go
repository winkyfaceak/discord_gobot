package serverstats

import (
	"testing"
	"time"
)

func TestSnapshotSortRosterAndPages(t *testing.T) {
	snapshot := &Snapshot{}
	for index := 0; index < 12; index++ {
		snapshot.Roster = append(snapshot.Roster, Player{
			Name:      string(rune('a' + index)),
			Score:     int32(index),
			Connected: time.Duration(index) * time.Minute,
		})
	}

	snapshot.SortRoster()

	if got, want := snapshot.PageCount(), 2; got != want {
		t.Fatalf("PageCount() = %d, want %d", got, want)
	}
	first := snapshot.RosterPage(0)
	second := snapshot.RosterPage(1)
	if len(first) != RowsPerPage || len(second) != 2 {
		t.Fatalf("page lengths = %d and %d, want %d and 2", len(first), len(second), RowsPerPage)
	}
	if first[0].Score != 11 || second[1].Score != 0 {
		t.Fatalf("roster was not sorted descending by score: first=%d last=%d", first[0].Score, second[1].Score)
	}
	if got := snapshot.ClampPage(99); got != 1 {
		t.Fatalf("ClampPage(99) = %d, want 1", got)
	}
}

func TestSnapshotEmptyRosterAlwaysHasOnePage(t *testing.T) {
	snapshot := &Snapshot{}
	if snapshot.PageCount() != 1 {
		t.Fatalf("empty PageCount() = %d, want 1", snapshot.PageCount())
	}
	if rows := snapshot.RosterPage(0); rows != nil {
		t.Fatalf("empty RosterPage(0) = %#v, want nil", rows)
	}
}
