package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"
	"discord_gobot/internal/navidrome"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/snowflake/v2"
)

// musicIdleTimeout is how long the bot waits in voice after the queue runs out.
const musicIdleTimeout = 5 * time.Minute

// Music implements /music: songs from Navidrome played in the caller's voice
// channel. Navidrome transcodes to Opus, so the bot only forwards packets.
type Music struct {
	nav *navidrome.Client

	mu      sync.Mutex
	players map[snowflake.ID]*musicPlayer
}

func NewMusic(nav *navidrome.Client) *Music {
	return &Music{nav: nav, players: make(map[snowflake.ID]*musicPlayer)}
}

func (m *Music) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "music",
		Description: "Play music from the home server in your voice channel",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionSubCommand{
				Name:        "play",
				Description: "Play or queue a song or album",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:         "search",
						Description:  "Song or album name",
						Required:     true,
						Autocomplete: true,
					},
				},
			},
			discord.ApplicationCommandOptionSubCommand{Name: "skip", Description: "Skip the current song"},
			discord.ApplicationCommandOptionSubCommand{Name: "queue", Description: "Show what's playing and up next"},
			discord.ApplicationCommandOptionSubCommand{Name: "stop", Description: "Stop and leave the voice channel"},
		},
	}
}

func (m *Music) Handle(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()
	if e.GuildID() == nil || data.SubCommandName == nil {
		return
	}
	guildID := *e.GuildID()

	switch *data.SubCommandName {
	case "play":
		m.play(e, guildID, data.String("search"))
	case "skip":
		p := m.player(guildID)
		if song, ok := p.skip(); ok {
			discordutil.Reply(e, "⏭️ Skipped "+songLabel(song), false)
		} else {
			discordutil.Reply(e, "Nothing is playing.", true)
		}
	case "queue":
		discordutil.Reply(e, m.player(guildID).describe(), false)
	case "stop":
		if m.stop(guildID) {
			discordutil.Reply(e, "⏹️ Stopped and left the voice channel.", false)
		} else {
			discordutil.Reply(e, "Nothing is playing.", true)
		}
	}
}

// HandleAutocomplete suggests albums and songs from the library as the user types.
func (m *Music) HandleAutocomplete(e *events.AutocompleteInteractionCreate) {
	choices := []discord.AutocompleteChoice{}
	query := strings.TrimSpace(e.Data.String("search"))
	if len([]rune(query)) >= 2 {
		// Discord drops autocomplete replies after 3 seconds
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		defer cancel()
		songs, albums, err := m.nav.Search(ctx, query, 15, 5)
		if err != nil {
			log.Printf("music: autocomplete search: %v", err)
		}
		for _, album := range albums {
			choices = append(choices, discord.AutocompleteChoiceString{Name: choiceName("💿 " + album.Name + " — " + album.Artist), Value: "album:" + album.ID})
		}
		for _, song := range songs {
			choices = append(choices, discord.AutocompleteChoiceString{Name: choiceName("🎵 " + song.Title + " — " + song.Artist), Value: "song:" + song.ID})
		}
	}
	if err := e.AutocompleteResult(choices); err != nil {
		log.Printf("music: autocomplete reply: %v", err)
	}
}

// OnEvent cleans up when the bot leaves voice for any reason (kicked,
// channel deleted, or our own /music stop).
func (m *Music) OnEvent(event bot.Event) {
	if e, ok := event.(*events.GuildVoiceLeave); ok && e.VoiceState.UserID == e.Client().ApplicationID {
		m.stop(e.VoiceState.GuildID)
	}
}

// Shutdown leaves every voice channel when the bot stops.
func (m *Music) Shutdown() {
	m.mu.Lock()
	guilds := make([]snowflake.ID, 0, len(m.players))
	for guildID := range m.players {
		guilds = append(guilds, guildID)
	}
	m.mu.Unlock()

	for _, guildID := range guilds {
		m.stop(guildID)
	}
}

func (m *Music) play(e *events.ApplicationCommandInteractionCreate, guildID snowflake.ID, search string) {
	// Installed with only the applications.commands scope, the app answers
	// commands but has no bot member, so it can't see or join voice
	if _, ok := e.Client().Caches.Guild(guildID); !ok {
		discordutil.Reply(e, "I'm installed here without my bot user, so I can't join voice. Re-invite me with the `bot` scope (see the README).", true)
		return
	}

	voiceState, ok := e.Client().Caches.VoiceState(guildID, e.User().ID)
	if !ok || voiceState.ChannelID == nil {
		discordutil.Reply(e, "Join a voice channel first, then run `/music play`.", true)
		return
	}

	if err := e.DeferCreateMessage(false); err != nil {
		log.Printf("music: defer /music play: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	songs, label, err := m.resolve(ctx, search)
	if err != nil {
		discordutil.EditText(e, "Couldn't search the library: "+err.Error())
		return
	}
	if len(songs) == 0 {
		discordutil.EditText(e, fmt.Sprintf("Nothing in the library matches `%s`.", search))
		return
	}

	p, problem := m.connect(ctx, e.Client(), guildID, *voiceState.ChannelID)
	if problem != "" {
		discordutil.EditText(e, problem)
		return
	}

	if position := p.enqueue(songs, e.Channel().ID()); position == 0 {
		discordutil.EditText(e, "▶️ Playing "+label)
	} else {
		discordutil.EditText(e, fmt.Sprintf("➕ Queued %s (position %d)", label, position))
	}
}

// resolve turns an autocomplete value ("song:<id>", "album:<id>") or free
// text into songs and a label for the reply.
func (m *Music) resolve(ctx context.Context, search string) ([]navidrome.Song, string, error) {
	kind, id, _ := strings.Cut(search, ":")
	switch kind {
	case "song":
		song, err := m.nav.Song(ctx, id)
		if err != nil {
			return nil, "", err
		}
		return []navidrome.Song{song}, songLabel(song), nil
	case "album":
		songs, err := m.nav.AlbumSongs(ctx, id)
		if err != nil || len(songs) == 0 {
			return nil, "", err
		}
		return songs, fmt.Sprintf("**%s** by %s (%d songs)", songs[0].Album, songs[0].Artist, len(songs)), nil
	}

	songs, _, err := m.nav.Search(ctx, search, 1, 0)
	if err != nil || len(songs) == 0 {
		return nil, "", err
	}
	return songs[:1], songLabel(songs[0]), nil
}

// connect returns the guild's player, joining channelID if not in voice yet.
// problem is a message for the user when that isn't possible.
func (m *Music) connect(ctx context.Context, client *bot.Client, guildID, channelID snowflake.ID) (p *musicPlayer, problem string) {
	// ponytail: one lock across guilds while joining; per-guild locks if this ever serves many servers
	m.mu.Lock()
	defer m.mu.Unlock()

	if p := m.players[guildID]; p != nil {
		current := p.conn.ChannelID()
		if current != nil && *current == channelID {
			return p, ""
		}
		if current != nil {
			return nil, fmt.Sprintf("I'm already playing in <#%s>. Join that channel, or `/music stop` first.", *current)
		}
		delete(m.players, guildID)
		p.shutdown()
	}

	conn := client.VoiceManager.CreateConn(guildID)
	if err := conn.Open(ctx, channelID, false, true); err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn.Close(closeCtx)
		return nil, fmt.Sprintf("Couldn't join the voice channel (%v). Does the bot have Connect and Speak permissions there?", err)
	}

	p = &musicPlayer{
		nav:    m.nav,
		client: client,
		conn:   conn,
		onIdle: func() { m.stop(guildID) },
	}
	conn.SetOpusFrameProvider(p)
	m.players[guildID] = p
	return p, ""
}

func (m *Music) player(guildID snowflake.ID) *musicPlayer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.players[guildID]
}

// stop ends playback and leaves voice. It reports whether anything was playing.
func (m *Music) stop(guildID snowflake.ID) bool {
	m.mu.Lock()
	p := m.players[guildID]
	delete(m.players, guildID)
	m.mu.Unlock()

	if p == nil {
		return false
	}
	p.shutdown()
	return true
}

// musicPlayer is one guild's queue. disgo pulls audio from it every 20ms.
type musicPlayer struct {
	nav    *navidrome.Client
	client *bot.Client
	conn   voice.Conn
	onIdle func()

	mu            sync.Mutex
	textChannelID snowflake.ID // where "now playing" cards go
	queue         []navidrome.Song
	current       *navidrome.Song
	stream        *navidrome.OpusStream
	idle          *time.Timer
	stopped       bool
	played        int // songs started, so card updates apply in order

	// The "now playing" card, edited for each new song
	cardMu      sync.Mutex
	cardChannel snowflake.ID
	cardMessage snowflake.ID
	cardShown   int
}

// ProvideOpusFrame returns the next 20ms Opus packet, moving through the
// queue as songs end. Nil means silence.
func (p *musicPlayer) ProvideOpusFrame() ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for !p.stopped {
		if p.stream == nil && !p.startNextLocked() {
			return nil, nil
		}
		packet, err := p.stream.Next()
		if err == nil {
			return packet, nil
		}
		if !errors.Is(err, io.EOF) {
			log.Printf("music: stream of %q failed: %v", p.current.Title, err)
		}
		p.endSongLocked()
	}
	return nil, nil
}

// Close is part of voice.OpusFrameProvider; shutdown does the cleanup.
func (p *musicPlayer) Close() {}

func (p *musicPlayer) startNextLocked() bool {
	for len(p.queue) > 0 {
		song := p.queue[0]
		p.queue = p.queue[1:]

		// No timeout on the context: it would cut the song off
		stream, err := p.nav.Stream(context.Background(), song.ID)
		if err != nil {
			log.Printf("music: start %q: %v", song.Title, err)
			continue
		}
		p.current, p.stream = &song, stream
		p.played++
		go p.announce(song, p.textChannelID, p.played)
		return true
	}

	if p.idle == nil {
		p.idle = time.AfterFunc(musicIdleTimeout, p.onIdle)
	}
	return false
}

func (p *musicPlayer) endSongLocked() {
	if p.stream != nil {
		p.stream.Close()
	}
	p.stream, p.current = nil, nil
}

// enqueue adds songs and returns the first one's queue position (0 = playing now).
func (p *musicPlayer) enqueue(songs []navidrome.Song, textChannelID snowflake.ID) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	position := len(p.queue)
	if p.current != nil {
		position++
	}
	p.queue = append(p.queue, songs...)
	p.textChannelID = textChannelID
	return position
}

func (p *musicPlayer) skip() (navidrome.Song, bool) {
	if p == nil {
		return navidrome.Song{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.current == nil {
		return navidrome.Song{}, false
	}
	song := *p.current
	p.endSongLocked()
	return song, true
}

func (p *musicPlayer) describe() string {
	if p == nil {
		return "Nothing is playing."
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.current == nil && len(p.queue) == 0 {
		return "Nothing is playing."
	}
	var b strings.Builder
	if p.current != nil {
		fmt.Fprintf(&b, "▶️ %s\n", songLabel(*p.current))
	}
	if len(p.queue) > 0 {
		b.WriteString("\n**Up next**\n")
	}
	for i, song := range p.queue[:min(len(p.queue), 10)] {
		fmt.Fprintf(&b, "%d. %s\n", i+1, songLabel(song))
	}
	if len(p.queue) > 10 {
		fmt.Fprintf(&b, "…and %d more\n", len(p.queue)-10)
	}
	return b.String()
}

// shutdown stops playback and leaves voice.
func (p *musicPlayer) shutdown() {
	p.mu.Lock()
	p.stopped = true
	p.queue = nil
	p.endSongLocked()
	if p.idle != nil {
		p.idle.Stop()
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.conn.Close(ctx)
}

// announce shows song (the number-th played) on the "now playing" card,
// editing the card already in that channel rather than posting one per song.
func (p *musicPlayer) announce(song navidrome.Song, channelID snowflake.ID, number int) {
	if channelID == 0 {
		return
	}
	embed := discord.Embed{
		Author:      &discord.EmbedAuthor{Name: "Now playing"},
		Title:       song.Title,
		Description: fmt.Sprintf("%s · %s", song.Artist, song.Album),
		Color:       0x5865f2,
		Footer:      &discord.EmbedFooter{Text: formatSongLength(song.Duration)},
	}
	var cover []byte
	var coverName string

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if art, err := p.nav.CoverArt(ctx, song.CoverArt, 300); err == nil && len(art) > 0 {
		cover, coverName = art, "cover.jpg"
		if http.DetectContentType(art) == "image/png" {
			coverName = "cover.png"
		}
		embed.Thumbnail = &discord.EmbedResource{URL: "attachment://" + coverName}
	}
	// Each send needs its own reader over the cover
	files := func() []*discord.File {
		if cover == nil {
			return nil
		}
		return []*discord.File{discord.NewFile(coverName, "Album cover", bytes.NewReader(cover))}
	}

	p.cardMu.Lock()
	defer p.cardMu.Unlock()
	if number <= p.cardShown {
		return // a later song already took the card (quick skips)
	}
	p.cardShown = number

	if p.cardMessage != 0 && p.cardChannel == channelID {
		update := discord.MessageUpdate{Embeds: &[]discord.Embed{embed}, Attachments: &[]discord.AttachmentUpdate{}, Files: files()}
		_, err := p.client.Rest.UpdateMessage(channelID, p.cardMessage, update)
		if err == nil {
			return
		}
		log.Printf("music: update now playing card (posting a new one): %v", err)
	}
	msg, err := p.client.Rest.CreateMessage(channelID, discord.MessageCreate{Embeds: []discord.Embed{embed}, Files: files()})
	if err != nil {
		log.Printf("music: post now playing: %v", err)
		return
	}
	p.cardChannel, p.cardMessage = channelID, msg.ID
}

func songLabel(song navidrome.Song) string {
	return fmt.Sprintf("**%s** — %s", song.Title, song.Artist)
}

func formatSongLength(seconds int) string {
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// choiceName fits Discord's 100-character limit for autocomplete choices.
func choiceName(name string) string {
	if runes := []rune(name); len(runes) > 100 {
		return string(runes[:99]) + "…"
	}
	return name
}
