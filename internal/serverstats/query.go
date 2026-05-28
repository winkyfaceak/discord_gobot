package serverstats

import (
	"context"
	"fmt"
	"time"

	a2s "github.com/rumblefrog/go-a2s"
)

const defaultQueryTimeout = 3 * time.Second

// Querier obtains live information for a validated endpoint.
type Querier interface {
	Query(ctx context.Context, endpoint Endpoint) (*Snapshot, error)
}

// A2SQuerier queries modern Source-compatible game servers over UDP.
type A2SQuerier struct {
	Timeout time.Duration
}

func NewA2SQuerier() *A2SQuerier {
	return &A2SQuerier{Timeout: defaultQueryTimeout}
}

func (q *A2SQuerier) Query(ctx context.Context, endpoint Endpoint) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	timeout := q.Timeout
	if timeout <= 0 {
		timeout = defaultQueryTimeout
	}

	client, err := a2s.NewClient(endpoint.Address, a2s.TimeoutOption(timeout))
	if err != nil {
		return nil, fmt.Errorf("open A2S client: %w", err)
	}
	defer client.Close()

	started := time.Now()
	info, err := client.QueryInfo()
	if err != nil {
		return nil, fmt.Errorf("A2S_INFO query failed: %w", err)
	}

	snapshot := &Snapshot{
		Endpoint:       endpoint,
		Name:           info.Name,
		Game:           info.Game,
		Map:            info.Map,
		AppID:          info.ID,
		CurrentPlayers: int(info.Players),
		MaxPlayers:     int(info.MaxPlayers),
		Bots:           int(info.Bots),
		VAC:            info.VAC,
		Password:       info.Visibility,
		FetchedAt:      time.Now(),
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	players, rosterErr := client.QueryPlayer()
	snapshot.Latency = time.Since(started)
	if rosterErr != nil {
		snapshot.RosterError = rosterErr.Error()
		return snapshot, nil
	}

	snapshot.RosterAvailable = true
	snapshot.Roster = make([]Player, 0, len(players.Players))
	for _, player := range players.Players {
		snapshot.Roster = append(snapshot.Roster, Player{
			Name:      player.Name,
			Score:     int32(player.Score),
			Connected: time.Duration(float64(player.Duration) * float64(time.Second)),
		})
	}
	snapshot.SortRoster()

	return snapshot, nil
}
