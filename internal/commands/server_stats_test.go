package commands

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	serverstatsapi "discord_gobot/internal/serverstats"

	"github.com/bwmarrin/discordgo"
)

type fakeServerStatsQueryResult struct {
	snapshot *serverstatsapi.Snapshot
	err      error
}

type fakeServerStatsQuerier struct {
	mu      sync.Mutex
	results []fakeServerStatsQueryResult
	calls   int
}

func (q *fakeServerStatsQuerier) Query(_ context.Context, _ serverstatsapi.Endpoint) (*serverstatsapi.Snapshot, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	index := q.calls
	q.calls++
	if index >= len(q.results) {
		index = len(q.results) - 1
	}
	return q.results[index].snapshot, q.results[index].err
}

type fakeServerStatsRenderer struct {
	mu    sync.Mutex
	views []serverstatsapi.CardView
}

func (r *fakeServerStatsRenderer) RenderPNG(_ context.Context, view serverstatsapi.CardView) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.views = append(r.views, view)
	return []byte("png"), nil
}

func (r *fakeServerStatsRenderer) statuses() []serverstatsapi.CardStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	statuses := make([]serverstatsapi.CardStatus, 0, len(r.views))
	for _, view := range r.views {
		statuses = append(statuses, view.Status)
	}
	return statuses
}

type fakeServerStatsUpdate struct {
	status     serverstatsapi.CardStatus
	components []discordgo.MessageComponent
}

type fakeServerStatsEditor struct {
	mu        sync.Mutex
	updates   []fakeServerStatsUpdate
	notify    chan struct{}
	updateErr error
}

func (e *fakeServerStatsEditor) create(_ *discordgo.Session, _ *discordgo.InteractionCreate, _ string, _ []byte, _ serverstatsapi.CardStatus, _ []discordgo.MessageComponent) (*discordgo.Message, error) {
	return &discordgo.Message{ID: "message", ChannelID: "channel"}, nil
}

func (e *fakeServerStatsEditor) update(_ *discordgo.Session, _ *discordgo.InteractionCreate, _ string, _ string, _ string, _ []byte, status serverstatsapi.CardStatus, components []discordgo.MessageComponent) error {
	e.mu.Lock()
	e.updates = append(e.updates, fakeServerStatsUpdate{status: status, components: components})
	e.mu.Unlock()
	if e.notify != nil {
		select {
		case e.notify <- struct{}{}:
		default:
		}
	}
	return e.updateErr
}

func newTestServerStatsCommand(q serverstatsapi.Querier, r serverstatsapi.Renderer, e serverStatsEditor) *ServerStats {
	command := NewServerStats(q, r)
	command.editor = e
	command.refreshEvery = time.Hour
	command.lifetime = time.Hour
	return command
}

func newTestServerStatsSession(id string, ownerID string) *serverStatsSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &serverStatsSession{
		id:        id,
		ownerID:   ownerID,
		endpoint:  serverstatsapi.Endpoint{Address: "203.0.113.10:27015"},
		channelID: "channel",
		messageID: "message",
		expiresAt: time.Now().Add(time.Hour),
		ctx:       ctx,
		cancel:    cancel,
		reset:     make(chan struct{}, 1),
	}
}

func TestServerStatsRefreshKeepsLastSnapshotWhenEndpointBecomesUnavailable(t *testing.T) {
	snapshot := &serverstatsapi.Snapshot{Name: "Server", RosterAvailable: true}
	querier := &fakeServerStatsQuerier{results: []fakeServerStatsQueryResult{
		{snapshot: snapshot},
		{err: errors.New("timeout")},
	}}
	renderer := &fakeServerStatsRenderer{}
	editor := &fakeServerStatsEditor{}
	command := newTestServerStatsCommand(querier, renderer, editor)
	session := newTestServerStatsSession("one", "owner")

	if err := command.refreshSession(session); err != nil {
		t.Fatalf("first refresh error = %v", err)
	}
	if err := command.refreshSession(session); err != nil {
		t.Fatalf("second refresh error = %v", err)
	}

	statuses := renderer.statuses()
	if len(statuses) != 2 || statuses[0] != serverstatsapi.StatusLive || statuses[1] != serverstatsapi.StatusStale {
		t.Fatalf("refresh statuses = %#v, want LIVE then STALE", statuses)
	}
	if session.snapshot != snapshot {
		t.Fatal("failed refresh did not retain the last successful snapshot")
	}
}

func TestServerStatsFinishClosesSessionAndRemovesControls(t *testing.T) {
	renderer := &fakeServerStatsRenderer{}
	editor := &fakeServerStatsEditor{}
	command := newTestServerStatsCommand(&fakeServerStatsQuerier{results: []fakeServerStatsQueryResult{{}}}, renderer, editor)
	session := newTestServerStatsSession("close-me", "owner")
	session.snapshot = &serverstatsapi.Snapshot{Name: "Server", RosterAvailable: true}
	command.activateSession(session)

	command.finishSession(session, serverstatsapi.StatusClosed)

	if command.lookupSession(session.id) != nil {
		t.Fatal("closed session remains registered")
	}
	statuses := renderer.statuses()
	if len(statuses) != 1 || statuses[0] != serverstatsapi.StatusClosed {
		t.Fatalf("finish statuses = %#v, want SESSION CLOSED", statuses)
	}
	editor.mu.Lock()
	defer editor.mu.Unlock()
	if len(editor.updates) != 1 || len(editor.updates[0].components) != 0 {
		t.Fatal("finished card did not remove controls")
	}
}

func TestServerStatsNewSessionReplacesPriorOwnerSession(t *testing.T) {
	renderer := &fakeServerStatsRenderer{}
	editor := &fakeServerStatsEditor{}
	command := newTestServerStatsCommand(&fakeServerStatsQuerier{results: []fakeServerStatsQueryResult{{}}}, renderer, editor)
	oldSession := newTestServerStatsSession("old", "owner")
	newSession := newTestServerStatsSession("new", "owner")
	command.activateSession(oldSession)

	replaced := command.activateSession(newSession)
	if replaced != oldSession {
		t.Fatal("activating a second owner session did not return the old session")
	}
	command.finishSession(replaced, serverstatsapi.StatusReplaced)

	if command.lookupSession("old") != nil || command.lookupSession("new") != newSession {
		t.Fatal("replacement session registration is incorrect")
	}
	if customID := serverStatsCustomID(newSession.id, "refresh"); strings.Contains(customID, newSession.endpoint.Address) {
		t.Fatal("component custom ID exposes the queried address")
	}
}

func TestServerStatsRunExpiresSession(t *testing.T) {
	renderer := &fakeServerStatsRenderer{}
	editor := &fakeServerStatsEditor{notify: make(chan struct{}, 1)}
	command := newTestServerStatsCommand(&fakeServerStatsQuerier{results: []fakeServerStatsQueryResult{{}}}, renderer, editor)
	session := newTestServerStatsSession("expiry", "owner")
	session.expiresAt = time.Now().Add(5 * time.Millisecond)
	command.activateSession(session)

	go command.runSession(session)

	select {
	case <-editor.notify:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for session expiry update")
	}

	statuses := renderer.statuses()
	if len(statuses) != 1 || statuses[0] != serverstatsapi.StatusExpired {
		t.Fatalf("expiry statuses = %#v, want SESSION EXPIRED", statuses)
	}
}

func TestServerStatsStopsPollingWhenMessageCanNoLongerBeEdited(t *testing.T) {
	snapshot := &serverstatsapi.Snapshot{Name: "Server", RosterAvailable: true}
	renderer := &fakeServerStatsRenderer{}
	editor := &fakeServerStatsEditor{updateErr: errors.New("message was deleted")}
	command := newTestServerStatsCommand(&fakeServerStatsQuerier{results: []fakeServerStatsQueryResult{{snapshot: snapshot}}}, renderer, editor)
	command.refreshEvery = time.Millisecond
	session := newTestServerStatsSession("gone", "owner")
	command.activateSession(session)

	go command.runSession(session)

	select {
	case <-session.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for failed edit to cancel polling")
	}
	if command.lookupSession(session.id) != nil {
		t.Fatal("uneditable session remains registered")
	}
}
