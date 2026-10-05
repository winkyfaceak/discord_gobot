package commands

import (
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// duelWait is how long a challenge waits to be accepted, and then how long
// the players have to pick (again after a draw).
const duelWait = 60 * time.Second

// Rock, paper, scissors: each hand beats the one before it.
var rpsHands = [3]string{"🪨 Rock", "📄 Paper", "✂️ Scissors"}

// Duel implements /duel: challenge someone to rock-paper-scissors for coins.
// Both stake the bet and pick in secret; the winner takes both stakes, and a
// draw is played again.
type Duel struct {
	casino *Casino

	mu    sync.Mutex
	duels map[string]*duel
}

type duel struct {
	id      string
	guildID snowflake.ID
	players [2]discord.User // challenger, opponent
	bet     int64

	mu       sync.Mutex
	accepted bool   // the opponent's stake is in too
	done     bool   // paid out or refunded
	picks    [2]int // index into rpsHands, -1 until picked
	deadline time.Time
	timer    *time.Timer

	// The /duel interaction, whose token edits the message when it times out
	client *bot.Client
	appID  snowflake.ID
	token  string
}

func (du *duel) Client() *bot.Client         { return du.client }
func (du *duel) ApplicationID() snowflake.ID { return du.appID }
func (du *duel) Token() string               { return du.token }

func NewDuel(casino *Casino) *Duel {
	return &Duel{casino: casino, duels: make(map[string]*duel)}
}

func (d *Duel) ComponentPrefix() string { return "duel" }

func (d *Duel) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "duel",
		Description: "Challenge someone to rock-paper-scissors for coins",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionUser{Name: "opponent", Description: "Who to challenge", Required: true},
			discord.ApplicationCommandOptionInt{Name: "bet", Description: "Coins each of you puts in", Required: true, MinValue: new(1)},
		},
	}
}

func (d *Duel) Handle(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()
	opponent := data.User("opponent")
	switch {
	case opponent.ID == e.User().ID:
		discordutil.Reply(e, "You can't duel yourself.", true)
		return
	case opponent.Bot:
		discordutil.Reply(e, "Bots don't duel. Pick a person.", true)
		return
	}

	du := &duel{
		id:       randomID(),
		guildID:  *e.GuildID(),
		players:  [2]discord.User{e.User(), opponent},
		bet:      int64(data.Int("bet")),
		picks:    [2]int{-1, -1},
		deadline: time.Now().Add(duelWait),
		client:   e.Client(),
		appID:    e.ApplicationID(),
		token:    e.Token(),
	}
	if _, problem := d.casino.take(du.guildID, du.players[0].ID, du.bet); problem != "" {
		discordutil.Reply(e, problem, true)
		return
	}
	du.timer = time.AfterFunc(duelWait, func() { d.expire(du) })
	d.mu.Lock()
	d.duels[du.id] = du
	d.mu.Unlock()

	du.mu.Lock()
	defer du.mu.Unlock()
	err := e.CreateMessage(discord.MessageCreate{
		Content:         du.text(""),
		Components:      du.buttons(),
		AllowedMentions: &discord.AllowedMentions{Parse: []discord.AllowedMentionType{}, Users: []snowflake.ID{opponent.ID}},
	})
	if err != nil {
		log.Printf("duel: challenge: %v", err)
		d.refundLocked(du)
	}
}

func (d *Duel) HandleComponent(e *events.ComponentInteractionCreate) {
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) != 3 {
		return
	}
	d.mu.Lock()
	du := d.duels[parts[1]]
	d.mu.Unlock()
	if du == nil {
		discordutil.Reply(e, "That duel is over. Start a new one with `/duel`.", true)
		return
	}

	du.mu.Lock()
	defer du.mu.Unlock()
	seat := slices.IndexFunc(du.players[:], func(u discord.User) bool { return u.ID == e.User().ID })
	switch {
	case du.done:
		discordutil.Reply(e, "That duel is over. Start a new one with `/duel`.", true)
		return
	case seat < 0:
		discordutil.Reply(e, "This duel isn't yours. Start your own with `/duel`.", true)
		return
	}

	switch action := parts[2]; action {
	case "accept":
		if seat != 1 {
			discordutil.Reply(e, fmt.Sprintf("Only %s can accept.", du.players[1].EffectiveName()), true)
			return
		}
		if du.accepted {
			return
		}
		if _, problem := d.casino.take(du.guildID, du.players[1].ID, du.bet); problem != "" {
			discordutil.Reply(e, problem, true)
			return
		}
		du.accepted = true
		du.restartClock()
		d.update(e, du.text(""), du.buttons())

	case "decline":
		if du.accepted {
			return
		}
		d.refundLocked(du)
		text := fmt.Sprintf("🏳️ <@%s> declined the duel. The bet was refunded.", du.players[1].ID)
		if seat == 0 {
			text = fmt.Sprintf("🏳️ <@%s> called off the duel. The bet was refunded.", du.players[0].ID)
		}
		d.update(e, text, nil)

	default:
		hand, err := strconv.Atoi(action)
		if err != nil || hand < 0 || hand >= len(rpsHands) || !du.accepted {
			return
		}
		du.picks[seat] = hand
		other := du.picks[1-seat]
		switch {
		case other < 0:
			d.update(e, du.text(""), du.buttons())
			discordutil.FollowUp(e, fmt.Sprintf("You picked **%s**. Click another to change it.", rpsHands[hand]), true)
		case other == hand:
			du.picks = [2]int{-1, -1}
			du.restartClock()
			d.update(e, du.text(fmt.Sprintf("Both threw **%s**! Draw, go again.", rpsHands[hand])), du.buttons())
		default:
			winner := 0
			if rpsBeats(du.picks[1], du.picks[0]) {
				winner = 1
			}
			loser := 1 - winner
			d.finishLocked(du)
			d.casino.give(du.guildID, du.players[winner].ID, 2*du.bet)
			d.update(e, fmt.Sprintf("🏆 <@%s>'s **%s** beats <@%s>'s **%s**! <@%s> takes the **%s** coin pot.",
				du.players[winner].ID, rpsHands[du.picks[winner]], du.players[loser].ID, rpsHands[du.picks[loser]],
				du.players[winner].ID, formatInt(int(2*du.bet))), nil)
		}
	}
}

// Shutdown refunds duels that haven't finished.
func (d *Duel) Shutdown() {
	d.mu.Lock()
	open := slices.Collect(maps.Values(d.duels))
	d.mu.Unlock()
	for _, du := range open {
		du.mu.Lock()
		d.refundLocked(du)
		du.mu.Unlock()
	}
}

// expire refunds a duel nobody finished in time.
func (d *Duel) expire(du *duel) {
	du.mu.Lock()
	defer du.mu.Unlock()
	if du.done || time.Now().Before(du.deadline) { // finished, or the clock restarted while this waited
		return
	}
	d.refundLocked(du)
	text := fmt.Sprintf("⌛ <@%s> didn't accept in time. The bet was refunded.", du.players[1].ID)
	if du.accepted {
		text = "⌛ The duel timed out before both players picked. Both bets were refunded."
	}
	if _, err := discordutil.EditOriginal(du, discord.MessageUpdate{Content: &text, Components: &[]discord.LayoutComponent{}}); err != nil {
		log.Printf("duel: expire: %v", err)
	}
}

func (d *Duel) update(e *events.ComponentInteractionCreate, text string, components []discord.LayoutComponent) {
	if components == nil {
		components = []discord.LayoutComponent{} // an empty list removes the buttons
	}
	if err := e.UpdateMessage(discord.MessageUpdate{Content: &text, Components: &components}); err != nil {
		log.Printf("duel: update: %v", err)
	}
}

// refundLocked gives back every stake put in and ends the duel.
func (d *Duel) refundLocked(du *duel) {
	if du.done {
		return
	}
	d.finishLocked(du)
	d.casino.give(du.guildID, du.players[0].ID, du.bet)
	if du.accepted {
		d.casino.give(du.guildID, du.players[1].ID, du.bet)
	}
}

func (d *Duel) finishLocked(du *duel) {
	du.done = true
	du.timer.Stop()
	d.mu.Lock()
	delete(d.duels, du.id)
	d.mu.Unlock()
}

func (du *duel) restartClock() {
	du.deadline = time.Now().Add(duelWait)
	du.timer.Reset(duelWait)
}

// text shows the challenge, or who has picked once it's accepted. note goes
// after the matchup line.
func (du *duel) text(note string) string {
	a, b := du.players[0].ID, du.players[1].ID
	if !du.accepted {
		return fmt.Sprintf("⚔️ <@%s> challenges <@%s> to rock-paper-scissors for **%s** coins each, winner takes both. The challenge ends <t:%d:R>.",
			a, b, formatInt(int(du.bet)), du.deadline.Unix())
	}
	var s strings.Builder
	fmt.Fprintf(&s, "⚔️ <@%s> vs <@%s> for a **%s** coin pot. %s\nPick in secret; time runs out <t:%d:R>.\n",
		a, b, formatInt(int(2*du.bet)), note, du.deadline.Unix())
	for seat, p := range du.players {
		status := "⏳ choosing…"
		if du.picks[seat] >= 0 {
			status = "✅ locked in"
		}
		fmt.Fprintf(&s, "<@%s>: %s\n", p.ID, status)
	}
	return s.String()
}

func (du *duel) buttons() []discord.LayoutComponent {
	prefix := "duel:" + du.id + ":"
	if !du.accepted {
		return []discord.LayoutComponent{discord.NewActionRow(
			discord.NewSuccessButton("Accept", prefix+"accept"),
			discord.NewSecondaryButton("Decline", prefix+"decline"),
		)}
	}
	var hands []discord.InteractiveComponent
	for i, name := range rpsHands {
		hands = append(hands, discord.NewSecondaryButton(name, prefix+strconv.Itoa(i)))
	}
	return []discord.LayoutComponent{discord.NewActionRow(hands...)}
}

// rpsBeats reports whether hand a beats hand b.
func rpsBeats(a, b int) bool { return (a-b+3)%3 == 1 }
