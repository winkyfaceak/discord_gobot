package deadlock

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Service contains the Deadlock statistics business logic.
type Service struct {
	client *Client
}

// NewService creates a Deadlock statistics service.
func NewService(client *Client) *Service {
	if client == nil {
		client = NewClient(nil)
	}

	return &Service{
		client: client,
	}
}

// LookupPlayer searches by account name and calculates player statistics.
func (s *Service) LookupPlayer(ctx context.Context, accountName string) (*PlayerSummary, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return nil, errors.New("account name cannot be empty")
	}

	profiles, err := s.client.SearchSteam(ctx, accountName)
	if err != nil {
		return nil, err
	}

	if len(profiles) == 0 {
		return nil, errors.New("no Steam profiles found for that name")
	}

	profile := bestProfileMatch(accountName, profiles)

	history, err := s.client.MatchHistory(ctx, profile.AccountID)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return nil, errors.New("profile found, but no Deadlock match history was returned")
	}

	summary := summarizeMatchHistory(profile, history)
	return &summary, nil
}

func bestProfileMatch(query string, profiles []SteamProfile) SteamProfile {
	query = strings.TrimSpace(strings.ToLower(query))

	for _, profile := range profiles {
		if strings.ToLower(strings.TrimSpace(profile.DisplayName())) == query {
			return profile
		}
	}

	return profiles[0]
}

func summarizeMatchHistory(profile SteamProfile, history []MatchHistoryEntry) PlayerSummary {
	var wins int
	var totalKills int64
	var totalDeaths int64
	var totalAssists int64
	var totalNetWorth int64
	var totalLastHits int64
	var totalDenies int64

	heroStats := map[int32]*heroAggregate{}

	recentLimit := 5
	if len(history) < recentLimit {
		recentLimit = len(history)
	}

	recentMatches := make([]RecentMatch, 0, recentLimit)

	for index, match := range history {
		won := didPlayerWin(match)

		if won {
			wins++
		}

		totalKills += int64(match.PlayerKills)
		totalDeaths += int64(match.PlayerDeaths)
		totalAssists += int64(match.PlayerAssists)
		totalNetWorth += int64(match.NetWorth)
		totalLastHits += int64(match.LastHits)
		totalDenies += int64(match.Denies)

		if _, ok := heroStats[match.HeroID]; !ok {
			heroStats[match.HeroID] = &heroAggregate{}
		}

		heroStats[match.HeroID].matches++
		if won {
			heroStats[match.HeroID].wins++
		}

		if index < recentLimit {
			recentMatches = append(recentMatches, RecentMatch{
				MatchID:      match.MatchID,
				HeroID:       match.HeroID,
				Won:          won,
				Kills:        match.PlayerKills,
				Deaths:       match.PlayerDeaths,
				Assists:      match.PlayerAssists,
				NetWorth:     match.NetWorth,
				LastHits:     match.LastHits,
				Denies:       match.Denies,
				DurationMins: match.MatchDuration / 60,
				StartedUnix:  match.StartTime,
			})
		}
	}

	topHeroID, topHeroMatches, topHeroWinRate := findTopHero(heroStats)

	matches := len(history)
	losses := matches - wins

	return PlayerSummary{
		AccountID:  profile.AccountID,
		Name:       profile.DisplayName(),
		ProfileURL: profile.ProfileURL,
		Avatar:     profile.Avatar,

		Matches: matches,
		Wins:    wins,
		Losses:  losses,
		WinRate: percent(wins, matches),

		AvgKills:    average(totalKills, matches),
		AvgDeaths:   average(totalDeaths, matches),
		AvgAssists:  average(totalAssists, matches),
		AvgNetWorth: average(totalNetWorth, matches),
		AvgLastHits: average(totalLastHits, matches),
		AvgDenies:   average(totalDenies, matches),

		TopHeroID:      topHeroID,
		TopHeroMatches: topHeroMatches,
		TopHeroWinRate: topHeroWinRate,

		RecentMatches: recentMatches,
	}
}

// didPlayerWin treats match_result as the winning team.
//
// If Deadlock API changes this encoding, this is the function to update.
func didPlayerWin(match MatchHistoryEntry) bool {
	return match.MatchResult == match.PlayerTeam
}

type heroAggregate struct {
	matches int
	wins    int
}

func findTopHero(heroStats map[int32]*heroAggregate) (heroID int32, matches int, winRate float64) {
	type row struct {
		heroID  int32
		matches int
		wins    int
	}

	rows := make([]row, 0, len(heroStats))
	for id, stats := range heroStats {
		rows = append(rows, row{
			heroID:  id,
			matches: stats.matches,
			wins:    stats.wins,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].matches == rows[j].matches {
			return percent(rows[i].wins, rows[i].matches) > percent(rows[j].wins, rows[j].matches)
		}

		return rows[i].matches > rows[j].matches
	})

	if len(rows) == 0 {
		return 0, 0, 0
	}

	top := rows[0]
	return top.heroID, top.matches, percent(top.wins, top.matches)
}

func average(total int64, count int) float64 {
	if count == 0 {
		return 0
	}

	return float64(total) / float64(count)
}

func percent(part int, total int) float64 {
	if total == 0 {
		return 0
	}

	return float64(part) / float64(total) * 100
}
