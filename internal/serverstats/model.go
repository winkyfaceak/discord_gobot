package serverstats

import (
	"sort"
	"time"
)

const RowsPerPage = 10

// Player is the portable player row returned by A2S_PLAYER.
type Player struct {
	Name      string
	Score     int32
	Connected time.Duration
}

// Snapshot contains server information gathered from one successful A2S_INFO
// request and, when permitted by the target server, A2S_PLAYER data.
type Snapshot struct {
	Endpoint        Endpoint
	Name            string
	Game            string
	Map             string
	AppID           uint16
	CurrentPlayers  int
	MaxPlayers      int
	Bots            int
	VAC             bool
	Password        bool
	Latency         time.Duration
	FetchedAt       time.Time
	Roster          []Player
	RosterAvailable bool
	RosterError     string
}

// SortRoster sorts visible rows as a scoreboard: score first, then duration.
func (s *Snapshot) SortRoster() {
	sort.SliceStable(s.Roster, func(i, j int) bool {
		if s.Roster[i].Score != s.Roster[j].Score {
			return s.Roster[i].Score > s.Roster[j].Score
		}
		if s.Roster[i].Connected != s.Roster[j].Connected {
			return s.Roster[i].Connected > s.Roster[j].Connected
		}
		return s.Roster[i].Name < s.Roster[j].Name
	})
}

// PageCount returns at least one page, including empty/unavailable rosters.
func (s *Snapshot) PageCount() int {
	if s == nil || len(s.Roster) == 0 {
		return 1
	}
	return (len(s.Roster) + RowsPerPage - 1) / RowsPerPage
}

// ClampPage normalizes a requested page for the current visible roster.
func (s *Snapshot) ClampPage(page int) int {
	if page < 0 {
		return 0
	}
	maxPage := s.PageCount() - 1
	if page > maxPage {
		return maxPage
	}
	return page
}

// RosterPage returns the score-sorted visible rows for one page.
func (s *Snapshot) RosterPage(page int) []Player {
	if s == nil || len(s.Roster) == 0 {
		return nil
	}
	page = s.ClampPage(page)
	start := page * RowsPerPage
	end := start + RowsPerPage
	if end > len(s.Roster) {
		end = len(s.Roster)
	}
	return s.Roster[start:end]
}
