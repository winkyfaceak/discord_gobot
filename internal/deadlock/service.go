package deadlock

import (
	"context"
	"errors"
	"sort"
	"strings"
)

const defaultRecentLimit = 5

// Service contains the Deadlock statistics business logic.
type Service struct {
	client *Client
	assets *AssetCache
}

// PlayerLookupOptions controls optional sections attached to a PlayerSummary.
type PlayerLookupOptions struct {
	RecentLimit    int
	IncludeRank    bool
	IncludeCurrent bool
	IncludeBuilds  bool
}

// NewService creates a Deadlock statistics service.
func NewService(client *Client) *Service {
	if client == nil {
		client = NewClient(nil)
	}

	return &Service{
		client: client,
		assets: NewAssetCache(client),
	}
}

// LookupPlayer searches by account name and calculates player statistics.
func (s *Service) LookupPlayer(ctx context.Context, accountName string) (*PlayerSummary, error) {
	return s.LookupPlayerWithOptions(ctx, accountName, PlayerLookupOptions{
		RecentLimit: defaultRecentLimit,
	})
}

// LookupPlayerWithOptions searches by account name, calculates player statistics,
// and optionally attaches rank/current-game/build sections.
func (s *Service) LookupPlayerWithOptions(ctx context.Context, accountName string, options PlayerLookupOptions) (*PlayerSummary, error) {
	profile, err := s.resolveProfile(ctx, accountName)
	if err != nil {
		return nil, err
	}

	return s.LookupPlayerByProfile(ctx, profile, options)
}

// LookupPlayerByProfile calculates player statistics for an already resolved profile.
func (s *Service) LookupPlayerByProfile(ctx context.Context, profile SteamProfile, options PlayerLookupOptions) (*PlayerSummary, error) {
	if profile.AccountID <= 0 {
		return nil, errors.New("account id cannot be empty")
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
	s.enrichSummaryAssets(ctx, &summary)

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

	if options.IncludeBuilds {
		build, buildErr := s.lookupBuildInsights(ctx, summary.AccountID, summary.TopHeroID)
		if buildErr != nil {
			summary.BuildError = buildErr.Error()
		} else {
			summary.Build = build
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
		if snapshot, assetErr := s.assets.Snapshot(ctx); assetErr == nil {
			asset := snapshot.Rank(prediction.Badge)
			prediction.Name = asset.Name
			prediction.ImageURL = asset.BestImageURL()
		}
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
		s.enrichActiveMatchAssets(ctx, &matchCopy)
		s.enrichActiveMatchSteamProfiles(ctx, &matchCopy)
		enrichedPlayer := matchCopy.PlayerForAccountID(profile.AccountID)
		if enrichedPlayer == nil {
			continue
		}

		playerCopy := *enrichedPlayer
		status.InGame = true
		status.Match = &matchCopy
		status.Player = &playerCopy
		return status, nil
	}

	return status, nil
}

func (s *Service) lookupBuildInsights(ctx context.Context, accountID int64, heroID int32) (*BuildInsight, error) {
	if heroID <= 0 {
		return nil, errors.New("no top hero was found for build lookup")
	}

	var snapshot *AssetSnapshot
	if assets, err := s.assets.Snapshot(ctx); err == nil {
		snapshot = assets
	}

	hero := AssetDetails{ID: heroID}
	if snapshot != nil {
		hero = snapshot.Hero(heroID)
	}

	buildStats, err := s.client.HeroBuildStats(ctx, heroID, accountID)
	playerFiltered := true
	if err != nil || len(buildStats) == 0 {
		// Fallback to global hero build stats so the page still gives useful build ideas.
		buildStats, err = s.client.HeroBuildStats(ctx, heroID, 0)
		playerFiltered = false
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(buildStats, func(i, j int) bool {
		if buildStats[i].Matches == buildStats[j].Matches {
			return percent64(buildStats[i].Wins, buildStats[i].Matches) > percent64(buildStats[j].Wins, buildStats[j].Matches)
		}
		return buildStats[i].Matches > buildStats[j].Matches
	})

	buildLimit := minInt(len(buildStats), 5)
	builds := make([]HeroBuildInsight, 0, buildLimit)
	for _, stat := range buildStats[:buildLimit] {
		builds = append(builds, HeroBuildInsight{
			HeroBuildID: stat.HeroBuildID,
			Wins:        stat.Wins,
			Losses:      stat.Losses,
			Matches:     stat.Matches,
			Players:     stat.Players,
			WinRate:     percent64(stat.Wins, stat.Matches),
		})
	}

	itemStats, itemErr := s.client.BuildItemStats(ctx, heroID)
	popularItems := []ItemBuildInsight{}
	if itemErr == nil {
		sort.Slice(itemStats, func(i, j int) bool {
			return itemStats[i].Builds > itemStats[j].Builds
		})

		itemLimit := minInt(len(itemStats), 8)
		popularItems = make([]ItemBuildInsight, 0, itemLimit)
		for _, stat := range itemStats[:itemLimit] {
			itemID := int32(stat.ItemID)
			item := AssetDetails{ID: itemID}
			if snapshot != nil {
				item = snapshot.Item(itemID)
			}
			popularItems = append(popularItems, ItemBuildInsight{
				ItemID:  itemID,
				Name:    item.DisplayName("Item"),
				IconURL: item.BestImageURL(),
				Builds:  stat.Builds,
			})
		}
	}

	note := "Player-filtered hero build stats."
	if !playerFiltered {
		note = "Global hero build stats fallback."
	}

	return &BuildInsight{
		HeroID:           heroID,
		HeroName:         hero.DisplayName("Hero"),
		HeroIconURL:      hero.BestImageURL(),
		PlayerFiltered:   playerFiltered,
		Builds:           builds,
		PopularItems:     popularItems,
		BuildsSourceNote: note,
	}, nil
}

func (s *Service) enrichSummaryAssets(ctx context.Context, summary *PlayerSummary) {
	snapshot, err := s.assets.Snapshot(ctx)
	if err != nil || snapshot == nil || summary == nil {
		return
	}

	if summary.TopHeroID > 0 {
		hero := snapshot.Hero(summary.TopHeroID)
		summary.TopHeroName = hero.DisplayName("Hero")
		summary.TopHeroIconURL = hero.BestImageURL()
	}

	for index := range summary.RecentMatches {
		match := &summary.RecentMatches[index]
		hero := snapshot.Hero(match.HeroID)
		match.HeroName = hero.DisplayName("Hero")
		match.HeroIconURL = hero.BestImageURL()
	}
}

func (s *Service) enrichActiveMatchAssets(ctx context.Context, match *ActiveMatch) {
	snapshot, err := s.assets.Snapshot(ctx)
	if err != nil || snapshot == nil || match == nil {
		return
	}

	for index := range match.Players {
		player := &match.Players[index]
		if player.HeroID == nil || *player.HeroID <= 0 {
			continue
		}
		hero := snapshot.Hero(*player.HeroID)
		player.HeroName = hero.DisplayName("Hero")
		player.HeroIconURL = hero.BestImageURL()
	}
}

func (s *Service) enrichActiveMatchSteamProfiles(ctx context.Context, match *ActiveMatch) {
	if match == nil || len(match.Players) == 0 {
		return
	}

	seen := map[int64]bool{}
	accountIDs := make([]int64, 0, len(match.Players))
	for _, player := range match.Players {
		if player.AccountID == nil || *player.AccountID <= 0 {
			continue
		}

		accountID := *player.AccountID
		if seen[accountID] {
			continue
		}

		seen[accountID] = true
		accountIDs = append(accountIDs, accountID)
	}

	profiles, err := s.client.SteamProfiles(ctx, accountIDs)
	if err != nil || len(profiles) == 0 {
		return
	}

	profilesByID := make(map[int64]SteamProfile, len(profiles))
	for _, profile := range profiles {
		profilesByID[profile.AccountID] = profile
	}

	for index := range match.Players {
		player := &match.Players[index]
		if player.AccountID == nil {
			continue
		}

		profile, ok := profilesByID[*player.AccountID]
		if !ok {
			continue
		}

		player.DisplayName = profile.DisplayName()
		player.ProfileURL = profile.ProfileURL
		player.Avatar = profile.Avatar
	}
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

func percent64(part int64, total int64) float64 {
	if total == 0 {
		return 0
	}

	return float64(part) / float64(total) * 100
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
