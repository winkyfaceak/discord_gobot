package commands

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"
	serverstatsapi "discord_gobot/internal/serverstats"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

const (
	serverStatsComponentPrefix = "serverstats"
	serverStatsRefreshInterval = 30 * time.Second
	serverStatsLifetime        = 15 * time.Minute
	serverStatsQueryTimeout    = 8 * time.Second
	serverStatsRenderTimeout   = 10 * time.Second
)

// ServerStats implements a live A2S server-scoreboard Discord session.
type ServerStats struct {
	querier  serverstatsapi.Querier
	renderer serverstatsapi.Renderer
	editor   serverStatsEditor

	now          func() time.Time
	newID        func() string
	refreshEvery time.Duration
	lifetime     time.Duration

	mu        sync.Mutex
	sessions  map[string]*serverStatsSession
	byOwnerID map[string]string
}

type serverStatsSession struct {
	id       string
	ownerID  string
	endpoint serverstatsapi.Endpoint
	// Where the card lives: the interaction token works for 15 minutes,
	// the channel/message IDs after that
	client    *bot.Client
	appID     snowflake.ID
	token     string
	channelID snowflake.ID
	messageID snowflake.ID
	expiresAt time.Time

	ctx    context.Context
	cancel context.CancelFunc
	reset  chan struct{}

	opMu        sync.Mutex
	snapshot    *serverstatsapi.Snapshot
	lastError   string
	lastAttempt time.Time
	page        int
	revision    int
	terminal    serverstatsapi.CardStatus
}

type serverStatsEditor interface {
	create(session *serverStatsSession, update discord.MessageUpdate) (*discord.Message, error)
	update(session *serverStatsSession, update discord.MessageUpdate) error
}

type discordServerStatsEditor struct{}

// NewServerStats creates the live Source server-query command.
func NewServerStats(querier serverstatsapi.Querier, renderer serverstatsapi.Renderer) *ServerStats {
	if querier == nil {
		querier = serverstatsapi.NewA2SQuerier()
	}
	if renderer == nil {
		renderer = serverstatsapi.ImageMagickRenderer{}
	}

	return &ServerStats{
		querier:      querier,
		renderer:     renderer,
		editor:       discordServerStatsEditor{},
		now:          time.Now,
		newID:        newServerStatsSessionID,
		refreshEvery: serverStatsRefreshInterval,
		lifetime:     serverStatsLifetime,
		sessions:     make(map[string]*serverStatsSession),
		byOwnerID:    make(map[string]string),
	}
}

func (c *ServerStats) ComponentPrefix() string {
	return serverStatsComponentPrefix
}

func (c *ServerStats) Definition() discord.SlashCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "server-stats",
		Description: "Open a live scoreboard for a public Valve/Steam Source server.",
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionString{
				Name:        "address",
				Description: "Public Source query endpoint, for example 203.0.113.10:27015.",
				Required:    true,
			},
		},
	}
}

func (c *ServerStats) Handle(e *events.ApplicationCommandInteractionCreate) {
	endpoint, err := serverstatsapi.ParseEndpoint(e.SlashCommandInteractionData().String("address"))
	if err != nil {
		discordutil.Reply(e, "Could not start server session: "+err.Error(), true)
		return
	}

	if err := e.DeferCreateMessage(false); err != nil {
		log.Printf("error deferring /server-stats: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := c.now()
	session := &serverStatsSession{
		id:        c.newID(),
		ownerID:   interactionUserID(e),
		endpoint:  endpoint,
		client:    e.Client(),
		appID:     e.ApplicationID(),
		token:     e.Token(),
		channelID: e.Channel().ID(),
		expiresAt: now.Add(c.lifetime),
		ctx:       ctx,
		cancel:    cancel,
		reset:     make(chan struct{}, 1),
	}

	session.opMu.Lock()
	c.queryLocked(session)
	card, renderErr := c.cardLocked(session, true)
	session.opMu.Unlock()
	if renderErr != nil {
		session.cancel()
		log.Printf("error rendering /server-stats initial card: %v", renderErr)
		discordutil.EditText(e, "Could not render the server scoreboard.")
		return
	}

	message, err := c.editor.create(session, card)
	if err != nil {
		session.cancel()
		log.Printf("error opening /server-stats session: %v", err)
		discordutil.EditText(e, "Could not open the server scoreboard session.")
		return
	}
	session.messageID = message.ID
	session.channelID = message.ChannelID

	previous := c.activateSession(session)
	if previous != nil {
		go c.finishSession(previous, serverstatsapi.StatusReplaced)
	}
	go c.runSession(session)
}

func (c *ServerStats) HandleComponent(e *events.ComponentInteractionCreate) {
	sessionID, action, ok := parseServerStatsCustomID(e.Data.CustomID())
	if !ok {
		discordutil.Reply(e, "That server session control is invalid.", true)
		return
	}

	session := c.lookupSession(sessionID)
	if session == nil {
		discordutil.Reply(e, "That server session has ended. Run `/server-stats` to open another.", true)
		return
	}
	if interactionUserID(e) != session.ownerID {
		discordutil.Reply(e, "Only the user who opened this server session can control it.", true)
		return
	}
	switch action {
	case "close", "refresh", "previous", "next":
	default:
		discordutil.Reply(e, "That server session action is not supported.", true)
		return
	}

	if err := e.DeferUpdateMessage(); err != nil {
		log.Printf("error deferring /server-stats button: %v", err)
		return
	}

	switch action {
	case "close":
		c.finishSession(session, serverstatsapi.StatusClosed)
	case "refresh":
		if err := c.refreshSession(session); err != nil {
			c.cancelUneditableSession(session, err)
			return
		}
		c.resetRefreshTimer(session)
	case "previous":
		if err := c.changePage(session, -1); err != nil {
			c.cancelUneditableSession(session, err)
		}
	case "next":
		if err := c.changePage(session, 1); err != nil {
			c.cancelUneditableSession(session, err)
		}
	}
}

// Shutdown cancels live polling when the bot is stopping.
func (c *ServerStats) Shutdown() {
	c.mu.Lock()
	sessions := make([]*serverStatsSession, 0, len(c.sessions))
	for _, session := range c.sessions {
		sessions = append(sessions, session)
	}
	c.sessions = make(map[string]*serverStatsSession)
	c.byOwnerID = make(map[string]string)
	c.mu.Unlock()

	for _, session := range sessions {
		session.cancel()
	}
}

func (c *ServerStats) activateSession(session *serverStatsSession) *serverStatsSession {
	c.mu.Lock()
	defer c.mu.Unlock()

	var previous *serverStatsSession
	if previousID := c.byOwnerID[session.ownerID]; previousID != "" {
		previous = c.sessions[previousID]
		delete(c.sessions, previousID)
	}
	c.sessions[session.id] = session
	c.byOwnerID[session.ownerID] = session.id
	return previous
}

func (c *ServerStats) lookupSession(id string) *serverStatsSession {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

func (c *ServerStats) detachSession(session *serverStatsSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[session.id] == session {
		delete(c.sessions, session.id)
	}
	if c.byOwnerID[session.ownerID] == session.id {
		delete(c.byOwnerID, session.ownerID)
	}
}

func (c *ServerStats) runSession(session *serverStatsSession) {
	refreshTimer := time.NewTimer(c.refreshEvery)
	expiryTimer := time.NewTimer(durationUntil(c.now(), session.expiresAt))
	defer refreshTimer.Stop()
	defer expiryTimer.Stop()

	for {
		select {
		case <-session.ctx.Done():
			return
		case <-expiryTimer.C:
			c.finishSession(session, serverstatsapi.StatusExpired)
			return
		case <-refreshTimer.C:
			if err := c.refreshSession(session); err != nil {
				c.cancelUneditableSession(session, err)
				return
			}
			refreshTimer.Reset(c.refreshEvery)
		case <-session.reset:
			if !refreshTimer.Stop() {
				select {
				case <-refreshTimer.C:
				default:
				}
			}
			refreshTimer.Reset(c.refreshEvery)
		}
	}
}

func (c *ServerStats) refreshSession(session *serverStatsSession) error {
	session.opMu.Lock()
	defer session.opMu.Unlock()
	if session.terminal != "" {
		return nil
	}
	c.queryLocked(session)
	return c.updateCardLocked(session, true)
}

func (c *ServerStats) changePage(session *serverStatsSession, delta int) error {
	session.opMu.Lock()
	defer session.opMu.Unlock()
	if session.terminal != "" {
		return nil
	}
	session.page += delta
	if session.snapshot != nil {
		session.page = session.snapshot.ClampPage(session.page)
	} else {
		session.page = 0
	}
	return c.updateCardLocked(session, true)
}

func (c *ServerStats) finishSession(session *serverStatsSession, status serverstatsapi.CardStatus) {
	c.detachSession(session)
	session.cancel()

	session.opMu.Lock()
	defer session.opMu.Unlock()
	if session.terminal != "" {
		return
	}
	session.terminal = status
	if err := c.updateCardLocked(session, false); err != nil {
		log.Printf("error finishing /server-stats session %s: %v", session.id, err)
	}
}

func (c *ServerStats) cancelUneditableSession(session *serverStatsSession, err error) {
	log.Printf("stopping /server-stats session %s after message update failed: %v", session.id, err)
	c.detachSession(session)
	session.cancel()
}

func (c *ServerStats) resetRefreshTimer(session *serverStatsSession) {
	select {
	case session.reset <- struct{}{}:
	default:
	}
}

func (c *ServerStats) queryLocked(session *serverStatsSession) {
	ctx, cancel := context.WithTimeout(session.ctx, serverStatsQueryTimeout)
	defer cancel()

	snapshot, err := c.querier.Query(ctx, session.endpoint)
	session.lastAttempt = c.now()
	if err != nil {
		session.lastError = err.Error()
		return
	}
	if snapshot == nil {
		session.lastError = "server query returned no snapshot"
		return
	}

	session.snapshot = snapshot
	session.lastError = ""
	session.page = snapshot.ClampPage(session.page)
}

func (c *ServerStats) updateCardLocked(session *serverStatsSession, controls bool) error {
	card, err := c.cardLocked(session, controls)
	if err != nil {
		return err
	}
	return c.editor.update(session, card)
}

func (c *ServerStats) cardLocked(session *serverStatsSession, controls bool) (discord.MessageUpdate, error) {
	status := sessionStatus(session)
	view := serverstatsapi.CardView{
		Endpoint:    session.endpoint,
		Snapshot:    session.snapshot,
		Status:      status,
		Detail:      session.lastError,
		Page:        session.page,
		LastAttempt: session.lastAttempt,
		ExpiresAt:   session.expiresAt,
	}

	renderCtx, cancel := context.WithTimeout(context.Background(), serverStatsRenderTimeout)
	defer cancel()
	imageBytes, err := c.renderer.RenderPNG(renderCtx, view)
	if err != nil {
		return discord.MessageUpdate{}, err
	}

	session.revision++
	filename := fmt.Sprintf("server-stats-%s-%03d.png", session.id, session.revision)
	var components []discord.LayoutComponent
	if controls {
		components = serverStatsComponents(session)
	}
	alt := fmt.Sprintf("Server scoreboard for %s (%s)", session.endpoint.Address, status)
	return discordutil.ImageUpdate(filename, alt, imageBytes, serverStatsEmbed(status), components), nil
}

func sessionStatus(session *serverStatsSession) serverstatsapi.CardStatus {
	if session.terminal != "" {
		return session.terminal
	}
	if session.lastError != "" {
		if session.snapshot != nil {
			return serverstatsapi.StatusStale
		}
		return serverstatsapi.StatusOffline
	}
	return serverstatsapi.StatusLive
}

func serverStatsComponents(session *serverStatsSession) []discord.LayoutComponent {
	page := 0
	pageCount := 1
	if session.snapshot != nil {
		page = session.snapshot.ClampPage(session.page)
		pageCount = session.snapshot.PageCount()
	}

	return []discord.LayoutComponent{
		discord.NewActionRow(
			discord.NewSecondaryButton("Previous", serverStatsCustomID(session.id, "previous")).WithDisabled(page <= 0),
			discord.NewSecondaryButton("Next", serverStatsCustomID(session.id, "next")).WithDisabled(page >= pageCount-1),
			discord.NewPrimaryButton("Refresh Now", serverStatsCustomID(session.id, "refresh")),
			discord.NewDangerButton("Close Session", serverStatsCustomID(session.id, "close")),
		),
	}
}

func serverStatsCustomID(sessionID string, action string) string {
	return fmt.Sprintf("%s:%s:%s", serverStatsComponentPrefix, sessionID, action)
}

func parseServerStatsCustomID(customID string) (string, string, bool) {
	parts := strings.Split(customID, ":")
	if len(parts) != 3 || parts[0] != serverStatsComponentPrefix || parts[1] == "" || parts[2] == "" {
		return "", "", false
	}
	return parts[1], parts[2], true
}

func newServerStatsSessionID() string {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err == nil {
		return hex.EncodeToString(bytes)
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func durationUntil(now time.Time, target time.Time) time.Duration {
	duration := target.Sub(now)
	if duration <= 0 {
		return time.Nanosecond
	}
	return duration
}

func (discordServerStatsEditor) create(session *serverStatsSession, update discord.MessageUpdate) (*discord.Message, error) {
	return session.client.Rest.UpdateInteractionResponse(session.appID, session.token, update)
}

func (discordServerStatsEditor) update(session *serverStatsSession, update discord.MessageUpdate) error {
	// The interaction token expires after 15 minutes; fall back to a plain message edit
	if _, err := session.client.Rest.UpdateInteractionResponse(session.appID, session.token, update); err == nil {
		return nil
	}
	_, err := session.client.Rest.UpdateMessage(session.channelID, session.messageID, update)
	return err
}

func serverStatsEmbed(status serverstatsapi.CardStatus) discord.Embed {
	color := 0xd46a23
	switch status {
	case serverstatsapi.StatusLive:
		color = 0x31814c
	case serverstatsapi.StatusOffline, serverstatsapi.StatusStale:
		color = 0xa34234
	}

	return discord.Embed{Color: color}
}
