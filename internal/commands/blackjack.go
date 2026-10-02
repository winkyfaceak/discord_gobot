package commands

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/blackjack"
	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// blackjackIdle is how long a hand waits for a button press before standing.
const blackjackIdle = 5 * time.Minute

// Blackjack implements /blackjack: one hand against the dealer, played with
// buttons, betting casino coins. One hand per player at a time.
type Blackjack struct {
	casino *Casino

	mu     sync.Mutex
	tables map[string]*blackjackTable
}

type blackjackTable struct {
	id      string
	guildID snowflake.ID
	player  discord.User

	mu   sync.Mutex // serialises button presses on this hand
	game *blackjack.Game
	idle *time.Timer

	// The latest interaction, whose token can still edit the message when
	// the hand times out.
	client *bot.Client
	appID  snowflake.ID
	token  string
}

// The table doubles as a discordutil.Interaction for edits after a timeout.
func (t *blackjackTable) Client() *bot.Client         { return t.client }
func (t *blackjackTable) ApplicationID() snowflake.ID { return t.appID }
func (t *blackjackTable) Token() string               { return t.token }

func NewBlackjack(casino *Casino) *Blackjack {
	return &Blackjack{casino: casino, tables: make(map[string]*blackjackTable)}
}

func (b *Blackjack) ComponentPrefix() string { return "blackjack" }

func (b *Blackjack) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "blackjack",
		Description: "Play a hand of blackjack against the dealer",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionInt{Name: "bet", Description: "How many coins to bet", Required: true, MinValue: new(1)},
		},
	}
}

func (b *Blackjack) Handle(e *events.ApplicationCommandInteractionCreate) {
	bet := int64(e.SlashCommandInteractionData().Int("bet"))
	t := &blackjackTable{
		id:      randomID(),
		guildID: *e.GuildID(),
		player:  e.User(),
		client:  e.Client(),
		appID:   e.ApplicationID(),
		token:   e.Token(),
	}

	if !b.claim(t) {
		discordutil.Reply(e, "Finish your current hand first.", true)
		return
	}
	if _, problem := b.casino.take(t.guildID, t.player.ID, bet); problem != "" {
		b.release(t)
		discordutil.Reply(e, problem, true)
		return
	}
	if err := e.DeferCreateMessage(false); err != nil {
		log.Printf("blackjack: defer: %v", err)
		b.release(t)
		b.casino.give(t.guildID, t.player.ID, bet)
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.game = blackjack.Deal(blackjack.NewShoe(6), bet)
	b.show(t, "")
}

func (b *Blackjack) HandleComponent(e *events.ComponentInteractionCreate) {
	_, rest, _ := strings.Cut(e.Data.CustomID(), ":")
	id, action, _ := strings.Cut(rest, ":")

	b.mu.Lock()
	t := b.tables[id]
	b.mu.Unlock()
	if t == nil {
		discordutil.Reply(e, "That hand is over. Start a new one with `/blackjack`.", true)
		return
	}
	if e.User().ID != t.player.ID {
		discordutil.Reply(e, fmt.Sprintf("This is %s's hand. Start your own with `/blackjack`.", t.player.EffectiveName()), true)
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.game.Done() {
		discordutil.Reply(e, "That hand is over. Start a new one with `/blackjack`.", true)
		return
	}
	if action == "double" {
		if _, problem := b.casino.take(t.guildID, t.player.ID, t.game.Bet); problem != "" {
			discordutil.Reply(e, problem, true)
			return
		}
	}
	if err := e.DeferUpdateMessage(); err != nil {
		log.Printf("blackjack: defer button: %v", err)
		if action == "double" {
			b.casino.give(t.guildID, t.player.ID, t.game.Bet)
		}
		return
	}

	switch action {
	case "hit":
		t.game.Hit()
	case "stand":
		t.game.Stand()
	case "double":
		t.game.Double()
	}
	t.appID, t.token = e.ApplicationID(), e.Token()
	b.show(t, "")
}

// Shutdown refunds hands still in play, since they can't be finished.
func (b *Blackjack) Shutdown() {
	b.mu.Lock()
	open := make([]*blackjackTable, 0, len(b.tables))
	for _, t := range b.tables {
		open = append(open, t)
	}
	b.mu.Unlock()

	for _, t := range open {
		t.mu.Lock()
		if t.game != nil && !t.game.Done() {
			b.casino.give(t.guildID, t.player.ID, t.game.Bet)
		}
		t.mu.Unlock()
	}
}

// show draws the table and edits the message. A finished hand is paid out
// and loses its buttons; one still in play gets a fresh idle timer.
// Called with t.mu held.
func (b *Blackjack) show(t *blackjackTable, note string) {
	name := t.player.EffectiveName()
	game := t.game
	var text string
	var components []discord.LayoutComponent

	if game.Done() {
		b.release(t)
		balance := b.casino.give(t.guildID, t.player.ID, game.Payout())
		result := fmt.Sprintf("Lost **%s** coins.", formatInt(int(game.Bet)))
		switch net := game.Payout() - game.Bet; {
		case net > 0:
			result = fmt.Sprintf("Won **%s** coins!", formatInt(int(net)))
		case net == 0:
			result = "Your bet is returned."
		}
		text = fmt.Sprintf("🃏 **%s** bet **%s**: %s %s Balance: **%s**%s", name, formatInt(int(game.Bet)), game.Summary(), result, formatInt(int(balance)), note)
	} else {
		text = fmt.Sprintf("🃏 **%s** bets **%s**. You have **%s**; the dealer shows **%s**.", name, formatInt(int(game.Bet)), game.Player.Label(), game.Dealer[0])
		canDouble := game.CanDouble() && b.casino.coins(t.guildID, t.player.ID) >= game.Bet
		components = []discord.LayoutComponent{discord.NewActionRow(
			discord.NewPrimaryButton("Hit", "blackjack:"+t.id+":hit"),
			discord.NewSecondaryButton("Stand", "blackjack:"+t.id+":stand"),
			discord.NewSuccessButton("Double", "blackjack:"+t.id+":double").WithDisabled(!canDouble),
		)}
		if t.idle != nil {
			t.idle.Stop()
		}
		t.idle = time.AfterFunc(blackjackIdle, func() { b.timeOut(t) })
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	update := discord.MessageUpdate{Content: &text, Components: &components}
	if png, err := game.RenderPNG(ctx, name); err == nil {
		update = discordutil.ImageUpdate("blackjack-"+t.id+".png", "Blackjack table", png, discord.Embed{Color: 0x11734a}, components)
		update.Content = &text
	} else {
		log.Printf("blackjack: render: %v", err)
	}
	if _, err := discordutil.EditOriginal(t, update); err != nil {
		log.Printf("blackjack: update hand: %v", err)
	}
}

func (b *Blackjack) timeOut(t *blackjackTable) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.game.Done() {
		return
	}
	t.game.Stand()
	b.show(t, " (stood automatically after 5 minutes)")
}

// claim registers a new hand unless the player already has one in this server.
func (b *Blackjack) claim(t *blackjackTable) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, other := range b.tables {
		if other.guildID == t.guildID && other.player.ID == t.player.ID {
			return false
		}
	}
	b.tables[t.id] = t
	return true
}

func (b *Blackjack) release(t *blackjackTable) {
	b.mu.Lock()
	delete(b.tables, t.id)
	b.mu.Unlock()
	if t.idle != nil {
		t.idle.Stop()
	}
}
