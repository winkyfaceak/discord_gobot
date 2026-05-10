package deadlock

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

const defaultRecentLimit = 5

// Service contains the Deadlock statistics business logic.
type Service struct {
	client *Client
}

// PlayerLookupOptions controls optional sections attached to a PlayerSummary.
type PlayerLookupOptions struct {
	RecentLimit    int
	IncludeRank    bool
	IncludeCurrent bool
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
	return s.LookupPlayerWithOptions(ctx, accountName, PlayerLookupOptions{
		RecentLimit: defaultRecentLimit,
	})
}

// LookupPlayerWithOptions searches by account name, calculates player statistics,
// and optionally attaches rank/current-game sections.
func (s *Service) LookupPlayerWithOptions(ctx context.Context, accountName string, options PlayerLookupOptions) (*PlayerSummary, error) {
	profile, err := s.resolveProfile(ctx, accountName)
	if err != nil {
		return nil, err
	}

	history, err := s.client.MatchHistory(ctx, profile.AccountID)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return nil, errors.New("profile found, but no Deadlock match history was returned")
	}

	if options.RecentLimit <= 0 {
		options.RecentLimit = defaultRecentLimit
	}

	summary := summarizeMatchHistory(profile, history, options.RecentLimit)

	if options.IncludeRank {
		rank, rankErr := s.lookupRankForProfile(ctx, profile)
		if rankErr != nil {
			summary.RankError = rankErr.Error()
		} else if rank != nil {
			summary.Rank = rank.Rank
		}
	}

	if options.IncludeCurrent {
		current, currentErr := s.lookupCurrentGameForProfile(ctx, profile)
		if currentErr != nil {
			summary.CurrentGameError = currentErr.Error()
		} else {
			summary.CurrentGame = current
		}
	}

	return &summary, nil
}

// LookupRank searches by account name and returns the player's predicted rank.
func (s *Service) LookupRank(ctx context.Context, accountName string) (*RankStatus, error) {
	profile, err := s.resolveProfile(ctx, accountName)
	if err != nil {
		return nil, err
	}

	return s.lookupRankForProfile(ctx, profile)
}

// LookupCurrentGame searches by account name and returns current-game status.
func (s *Service) LookupCurrentGame(ctx context.Context, accountName string) (*CurrentGameStatus, error) {
	profile, err := s.resolveProfile(ctx, accountName)
	if err != nil {
		return nil, err
	}

	return s.lookupCurrentGameForProfile(ctx, profile)
}

func (s *Service) resolveProfile(ctx context.Context, accountName string) (SteamProfile, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return SteamProfile{}, errors.New("account name cannot be empty")
	}

	profiles, err := s.client.SearchSteam(ctx, accountName)
	if err != nil {
		return SteamProfile{}, err
	}

	if len(profiles) == 0 {
		return SteamProfile{}, errors.New("no Steam profiles found for that name")
	}

	return bestProfileMatch(accountName, profiles), nil
}

func (s *Service) lookupRankForProfile(ctx context.Context, profile SteamProfile) (*RankStatus, error) {
	prediction, err := s.client.RankPrediction(ctx, profile.AccountID)
	if err != nil {
		return nil, err
	}

	if prediction != nil && prediction.Badge > 0 {
		name, imageURL := s.rankAssetDetails(ctx, prediction.Badge)
		prediction.Name = name
		prediction.ImageURL = imageURL
	}

	return &RankStatus{
		AccountID:  profile.AccountID,
		Name:       profile.DisplayName(),
		ProfileURL: profile.ProfileURL,
		Avatar:     profile.Avatar,
		Rank:       prediction,
	}, nil
}

func (s *Service) lookupCurrentGameForProfile(ctx context.Context, profile SteamProfile) (*CurrentGameStatus, error) {
	matches, err := s.client.ActiveMatches(ctx, profile.AccountID)
	if err != nil {
		return nil, err
	}

	status := &CurrentGameStatus{
		AccountID:  profile.AccountID,
		Name:       profile.DisplayName(),
		ProfileURL: profile.ProfileURL,
		Avatar:     profile.Avatar,
	}

	for _, match := range matches {
		player := match.PlayerForAccountID(profile.AccountID)
		if player == nil {
			continue
		}

		matchCopy := match
		playerCopy := *player
		status.InGame = true
		status.Match = &matchCopy
		status.Player = &playerCopy
		return status, nil
	}

	return status, nil
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

func summarizeMatchHistory(profile SteamProfile, history []MatchHistoryEntry, recentLimit int) PlayerSummary {
	var wins int
	var totalKills int64
	var totalDeaths int64
	var totalAssists int64
	var totalNetWorth int64
	var totalLastHits int64
	var totalDenies int64

	heroStats := map[int32]*heroAggregate{}

	if recentLimit <= 0 {
		recentLimit = defaultRecentLimit
	}
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

func (s *Service) rankAssetDetails(ctx context.Context, badge int32) (name string, imageURL string) {
	payload, err := s.client.RankAssets(ctx)
	if err != nil {
		return "", ""
	}

	name, imageURL = findRankAsset(payload, badge)
	return name, imageURL
}

func findRankAsset(payload any, badge int32) (name string, imageURL string) {
	var walk func(value any) bool

	walk = func(value any) bool {
		switch typed := value.(type) {
		case []any:
			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		case map[string]any:
			if rankMapMatches(typed, badge) {
				name = firstRankName(typed)
				imageURL = bestImageURL(typed)
				if name != "" || imageURL != "" {
					return true
				}
			}

			for _, child := range typed {
				if walk(child) {
					return true
				}
			}
		}

		return false
	}

	walk(payload)
	return name, imageURL
}

func rankMapMatches(values map[string]any, badge int32) bool {
	tier := badge / 10

	for key, value := range values {
		lowerKey := strings.ToLower(key)
		number, ok := numberAsInt64(value)
		if !ok {
			continue
		}

		if strings.Contains(lowerKey, "badge") && number == int64(badge) {
			return true
		}

		if (strings.Contains(lowerKey, "rank") || strings.Contains(lowerKey, "tier")) && number == int64(tier) {
			return true
		}

		if lowerKey == "id" && (number == int64(badge) || number == int64(tier)) {
			return true
		}
	}

	return false
}

func firstRankName(values map[string]any) string {
	preferredKeys := []string{"name", "display_name", "rank_name", "title"}

	for _, preferredKey := range preferredKeys {
		for key, value := range values {
			if strings.EqualFold(key, preferredKey) {
				if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
		}
	}

	return ""
}

func bestImageURL(value any) string {
	type candidate struct {
		url   string
		score int
	}

	var candidates []candidate

	var walk func(value any, keyHint string)
	walk = func(value any, keyHint string) {
		switch typed := value.(type) {
		case string:
			if url := normalizeImageURL(typed); url != "" {
				score := 1
				lowerKey := strings.ToLower(keyHint)
				lowerURL := strings.ToLower(url)

				for _, token := range []string{"large", "big", "image", "icon", "badge", "rank"} {
					if strings.Contains(lowerKey, token) || strings.Contains(lowerURL, token) {
						score++
					}
				}

				candidates = append(candidates, candidate{url: url, score: score})
			}
		case []any:
			for _, child := range typed {
				walk(child, keyHint)
			}
		case map[string]any:
			for key, child := range typed {
				walk(child, key)
			}
		}
	}

	walk(value, "")

	if len(candidates) == 0 {
		return ""
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates[0].url
}

func normalizeImageURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	lower := strings.ToLower(value)
	isImage := strings.Contains(lower, ".png") ||
		strings.Contains(lower, ".jpg") ||
		strings.Contains(lower, ".jpeg") ||
		strings.Contains(lower, ".webp")
	if !isImage {
		return ""
	}

	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return value
	}

	if strings.HasPrefix(value, "/") {
		return "https://assets.deadlock-api.com" + value
	}

	return ""
}

func numberAsInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if math.Trunc(typed) == typed {
			return int64(typed), true
		}
	case float32:
		float := float64(typed)
		if math.Trunc(float) == float {
			return int64(typed), true
		}
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed, true
		}
	}

	return 0, false
}
