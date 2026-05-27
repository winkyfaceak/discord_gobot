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

	"github.com/bwmarrin/discordgo"
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
	id          string
	ownerID     string
	endpoint    serverstatsapi.Endpoint
	discord     *discordgo.Session
	interaction *discordgo.InteractionCreate
	channelID   string
	messageID   string
	expiresAt   time.Time

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
	create(
		s *discordgo.Session,
		i *discordgo.InteractionCreate,
		filename string,
		imageBytes []byte,
		status serverstatsapi.CardStatus,
		components []discordgo.MessageComponent,
	) (*discordgo.Message, error)
	update(
		s *discordgo.Session,
		i *discordgo.InteractionCreate,
		channelID string,
		messageID string,
		filename string,
		imageBytes []byte,
		status serverstatsapi.CardStatus,
		components []discordgo.MessageComponent,
	) error
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

func (c *ServerStats) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "server-stats",
		Description: "Open a live scoreboard for a public Valve/Steam Source server.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "address",
				Description: "Public Source query endpoint, for example 203.0.113.10:27015.",
				Required:    true,
			},
		},
	}
}

func (c *ServerStats) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := discordutil.OptionMap(i.ApplicationCommandData().Options)
	addressOption, ok := options["address"]
	if !ok {
		discordutil.Respond(s, i, "Missing required input: address", true)
		return
	}

	endpoint, err := serverstatsapi.ParseEndpoint(addressOption.StringValue())
	if err != nil {
		discordutil.Respond(s, i, "Could not start server session: "+err.Error(), true)
		return
	}

	ownerID := interactionUserID(i)
	if ownerID == "" {
		discordutil.Respond(s, i, "Could not identify the user opening this server session.", true)
		return
	}

	discordutil.Defer(s, i, false)

	ctx, cancel := context.WithCancel(context.Background())
	now := c.now()
	session := &serverStatsSession{
		id:          c.newID(),
		ownerID:     ownerID,
		endpoint:    endpoint,
		discord:     s,
		interaction: i,
		channelID:   i.ChannelID,
		expiresAt:   now.Add(c.lifetime),
		ctx:         ctx,
		cancel:      cancel,
		reset:       make(chan struct{}, 1),
	}

	session.opMu.Lock()
	c.queryLocked(session)
	filename, imageBytes, status, components, renderErr := c.cardPayloadLocked(session, true)
	session.opMu.Unlock()
	if renderErr != nil {
		session.cancel()
		log.Printf("error rendering /server-stats initial card: %v", renderErr)
		discordutil.EditOriginal(s, i, "Could not render the server scoreboard.")
		return
	}

	message, err := c.editor.create(s, i, filename, imageBytes, status, components)
	if err != nil {
		session.cancel()
		log.Printf("error opening /server-stats session: %v", err)
		discordutil.EditOriginal(s, i, "Could not open the server scoreboard session.")
		return
	}
	if message == nil || message.ID == "" {
		session.cancel()
		log.Printf("error opening /server-stats session: Discord response did not include a message ID")
		discordutil.EditOriginal(s, i, "Could not open the server scoreboard session.")
		return
	}
	session.messageID = message.ID
	if message.ChannelID != "" {
		session.channelID = message.ChannelID
	}

	previous := c.activateSession(session)
	if previous != nil {
		go c.finishSession(previous, serverstatsapi.StatusReplaced)
	}
	go c.runSession(session)
}

func (c *ServerStats) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	sessionID, action, ok := parseServerStatsCustomID(i.MessageComponentData().CustomID)
	if !ok {
		discordutil.RespondEphemeralToComponent(s, i, "That server session control is invalid.")
		return
	}

	session := c.lookupSession(sessionID)
	if session == nil {
		discordutil.RespondEphemeralToComponent(s, i, "That server session has ended. Run `/server-stats` to open another.")
		return
	}
	if clickerID := interactionUserID(i); clickerID == "" || clickerID != session.ownerID {
		discordutil.RespondEphemeralToComponent(s, i, "Only the user who opened this server session can control it.")
		return
	}
	switch action {
	case "close", "refresh", "previous", "next":
	default:
		discordutil.RespondEphemeralToComponent(s, i, "That server session action is not supported.")
		return
	}

	discordutil.DeferComponentUpdate(s, i)

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
	filename, imageBytes, status, components, err := c.cardPayloadLocked(session, controls)
	if err != nil {
		return err
	}
	return c.editor.update(session.discord, session.interaction, session.channelID, session.messageID, filename, imageBytes, status, components)
}

func (c *ServerStats) cardPayloadLocked(session *serverStatsSession, controls bool) (string, []byte, serverstatsapi.CardStatus, []discordgo.MessageComponent, error) {
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
		return "", nil, status, nil, err
	}

	session.revision++
	filename := fmt.Sprintf("server-stats-%s-%03d.png", session.id, session.revision)
	var components []discordgo.MessageComponent
	if controls {
		components = serverStatsComponents(session)
	}
	return filename, imageBytes, status, components, nil
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

func serverStatsComponents(session *serverStatsSession) []discordgo.MessageComponent {
	page := 0
	pageCount := 1
	if session.snapshot != nil {
		page = session.snapshot.ClampPage(session.page)
		pageCount = session.snapshot.PageCount()
	}

	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    "Previous",
				Style:    discordgo.SecondaryButton,
				CustomID: serverStatsCustomID(session.id, "previous"),
				Disabled: page <= 0,
			},
			discordgo.Button{
				Label:    "Next",
				Style:    discordgo.SecondaryButton,
				CustomID: serverStatsCustomID(session.id, "next"),
				Disabled: page >= pageCount-1,
			},
			discordgo.Button{
				Label:    "Refresh Now",
				Style:    discordgo.PrimaryButton,
				CustomID: serverStatsCustomID(session.id, "refresh"),
			},
			discordgo.Button{
				Label:    "Close Session",
				Style:    discordgo.DangerButton,
				CustomID: serverStatsCustomID(session.id, "close"),
			},
		}},
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

func (discordServerStatsEditor) create(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	filename string,
	imageBytes []byte,
	status serverstatsapi.CardStatus,
	components []discordgo.MessageComponent,
) (*discordgo.Message, error) {
	return discordutil.EditOriginalImageWithComponents(s, i, filename, imageBytes, serverStatsEmbed(filename, status), components)
}

func (discordServerStatsEditor) update(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	channelID string,
	messageID string,
	filename string,
	imageBytes []byte,
	status serverstatsapi.CardStatus,
	components []discordgo.MessageComponent,
) error {
	if i != nil {
		if _, err := discordutil.EditOriginalImageWithComponents(s, i, filename, imageBytes, serverStatsEmbed(filename, status), components); err == nil {
			return nil
		}
	}

	_, err := discordutil.EditChannelImageWithComponents(s, channelID, messageID, filename, imageBytes, serverStatsEmbed(filename, status), components)
	return err
}

func serverStatsEmbed(filename string, status serverstatsapi.CardStatus) *discordgo.MessageEmbed {
	color := 0xd46a23
	switch status {
	case serverstatsapi.StatusLive:
		color = 0x31814c
	case serverstatsapi.StatusOffline, serverstatsapi.StatusStale:
		color = 0xa34234
	}

	return &discordgo.MessageEmbed{
		Color: color,
		Image: &discordgo.MessageEmbedImage{URL: "attachment://" + filename},
	}
}
