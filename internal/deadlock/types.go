package deadlock

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

// RecentMatch is a smaller display-focused match shape.
type RecentMatch struct {
	MatchID      int64
	HeroID       int32
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
	TopHeroMatches int
	TopHeroWinRate float64

	RecentMatches []RecentMatch
}
