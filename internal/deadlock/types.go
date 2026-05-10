package deadlock

import "fmt"

// SteamProfile represents a Steam profile returned by Deadlock API search.
type SteamProfile struct {
	AccountID int64 `json:"account_id"`

	// The API/client ecosystem has used slightly different Steam persona field
	// names, so we support both.
	Persona     string `json:"personaname"`
	PersonaName string `json:"persona_name"`

	ProfileURL string `json:"profileurl"`
	Avatar     string `json:"avatarfull"`
}

func (p SteamProfile) DisplayName() string {
	if p.Persona != "" {
		return p.Persona
	}

	if p.PersonaName != "" {
		return p.PersonaName
	}

	if p.AccountID > 0 {
		return fmt.Sprintf("Account %d", p.AccountID)
	}

	return "Unknown Player"
}

// MatchHistoryEntry represents one Deadlock match-history row.
type MatchHistoryEntry struct {
	AccountID     int64 `json:"account_id"`
	Denies        int32 `json:"denies"`
	GameMode      int32 `json:"game_mode"`
	HeroID        int32 `json:"hero_id"`
	HeroLevel     int32 `json:"hero_level"`
	LastHits      int32 `json:"last_hits"`
	MatchDuration int32 `json:"match_duration_s"`
	MatchID       int64 `json:"match_id"`
	MatchMode     int32 `json:"match_mode"`
	MatchResult   int32 `json:"match_result"`
	NetWorth      int32 `json:"net_worth"`
	PlayerAssists int32 `json:"player_assists"`
	PlayerDeaths  int32 `json:"player_deaths"`
	PlayerKills   int32 `json:"player_kills"`
	PlayerTeam    int32 `json:"player_team"`
	StartTime     int64 `json:"start_time"`
}

// AssetDetails is the small static-asset shape the bot needs for Discord UI.
type AssetDetails struct {
	ID       int32
	Name     string
	IconURL  string
	ImageURL string
}

func (a AssetDetails) DisplayName(fallback string) string {
	if a.Name != "" {
		return a.Name
	}
	return fallback
}

func (a AssetDetails) BestImageURL() string {
	if a.IconURL != "" {
		return a.IconURL
	}
	return a.ImageURL
}

// RecentMatch is a smaller display-focused match shape.
type RecentMatch struct {
	MatchID      int64
	HeroID       int32
	HeroName     string
	HeroIconURL  string
	Won          bool
	Kills        int32
	Deaths       int32
	Assists      int32
	NetWorth     int32
	LastHits     int32
	Denies       int32
	DurationMins int32
	StartedUnix  int64
}

// RankPrediction is returned by /v1/players/{account_id}/rank-predict.
type RankPrediction struct {
	Badge       int32   `json:"badge"`
	RawScore    float64 `json:"raw_score"`
	MatchesUsed int     `json:"matches_used"`

	// Filled from the static assets API when available.
	Name     string
	ImageURL string
}

func (r RankPrediction) Tier() int32 {
	return r.Badge / 10
}

func (r RankPrediction) SubRank() int32 {
	return r.Badge % 10
}

func (r RankPrediction) DisplayName() string {
	if r.Name != "" {
		if r.SubRank() > 0 {
			return fmt.Sprintf("%s %d", r.Name, r.SubRank())
		}
		return r.Name
	}

	if r.Badge > 0 {
		return fmt.Sprintf("Badge %d (Tier %d / Sub-rank %d)", r.Badge, r.Tier(), r.SubRank())
	}

	return "Unknown rank"
}

// RankStatus is a display object for a player's predicted rank.
type RankStatus struct {
	AccountID  int64
	Name       string
	ProfileURL string
	Avatar     string
	Rank       *RankPrediction
}

// ActiveMatch represents one match from /v1/matches/active.
type ActiveMatch struct {
	StartTime          *int64              `json:"start_time"`
	WinningTeam        *int32              `json:"winning_team"`
	WinningTeamParsed  string              `json:"winning_team_parsed"`
	MatchID            *int64              `json:"match_id"`
	Players            []ActiveMatchPlayer `json:"players"`
	LobbyID            *int64              `json:"lobby_id"`
	GameModeVersion    *int32              `json:"game_mode_version"`
	NetWorthTeam0      *int32              `json:"net_worth_team_0"`
	NetWorthTeam1      *int32              `json:"net_worth_team_1"`
	DurationS          *int32              `json:"duration_s"`
	Spectators         *int32              `json:"spectators"`
	OpenSpectatorSlots *int32              `json:"open_spectator_slots"`
	MatchMode          *int32              `json:"match_mode"`
	MatchModeParsed    string              `json:"match_mode_parsed"`
	GameMode           *int32              `json:"game_mode"`
	GameModeParsed     string              `json:"game_mode_parsed"`
	MatchScore         *int32              `json:"match_score"`
	RegionMode         *int32              `json:"region_mode"`
	RegionModeParsed   string              `json:"region_mode_parsed"`
}

func (m ActiveMatch) PlayerForAccountID(accountID int64) *ActiveMatchPlayer {
	for index := range m.Players {
		player := &m.Players[index]
		if player.AccountID != nil && *player.AccountID == accountID {
			return player
		}
	}

	return nil
}

func (m ActiveMatch) NetWorthForTeam(team int32) *int32 {
	switch team {
	case 0:
		return m.NetWorthTeam0
	case 1:
		return m.NetWorthTeam1
	default:
		return nil
	}
}

// ActiveMatchPlayer is a player row inside an active match.
type ActiveMatchPlayer struct {
	AccountID  *int64 `json:"account_id"`
	Team       *int32 `json:"team"`
	TeamParsed string `json:"team_parsed"`
	Abandoned  *bool  `json:"abandoned"`
	HeroID     *int32 `json:"hero_id"`

	// Filled from the static assets API when available.
	HeroName    string `json:"-"`
	HeroIconURL string `json:"-"`
}

// CurrentGameStatus is a display object for active/current game lookup.
type CurrentGameStatus struct {
	AccountID  int64
	Name       string
	ProfileURL string
	Avatar     string
	InGame     bool
	Match      *ActiveMatch
	Player     *ActiveMatchPlayer
}

// BuildItemStats is returned by /v1/analytics/build-item-stats.
type BuildItemStats struct {
	Builds int64 `json:"builds"`
	ItemID int64 `json:"item_id"`
}

// HeroBuildStats is returned by /v1/analytics/hero-build-stats/{hero_id}.
type HeroBuildStats struct {
	HeroBuildID int64 `json:"hero_build_id"`
	HeroID      int32 `json:"hero_id"`
	Wins        int64 `json:"wins"`
	Losses      int64 `json:"losses"`
	Matches     int64 `json:"matches"`
	Players     int64 `json:"players"`
}

// BuildInsight is the Discord-display object for the Builds/Items page.
type BuildInsight struct {
	HeroID           int32
	HeroName         string
	HeroIconURL      string
	PlayerFiltered   bool
	Builds           []HeroBuildInsight
	PopularItems     []ItemBuildInsight
	BuildsSourceNote string
}

type HeroBuildInsight struct {
	HeroBuildID int64
	Wins        int64
	Losses      int64
	Matches     int64
	Players     int64
	WinRate     float64
}

type ItemBuildInsight struct {
	ItemID  int32
	Name    string
	IconURL string
	Builds  int64
}

// PlayerSummary is the final calculated statistics object.
type PlayerSummary struct {
	AccountID  int64
	Name       string
	ProfileURL string
	Avatar     string

	Matches int
	Wins    int
	Losses  int
	WinRate float64

	AvgKills    float64
	AvgDeaths   float64
	AvgAssists  float64
	AvgNetWorth float64
	AvgLastHits float64
	AvgDenies   float64

	TopHeroID      int32
	TopHeroName    string
	TopHeroIconURL string
	TopHeroMatches int
	TopHeroWinRate float64

	RecentMatches []RecentMatch

	Rank             *RankPrediction
	RankError        string
	CurrentGame      *CurrentGameStatus
	CurrentGameError string
	Build            *BuildInsight
	BuildError       string
}
