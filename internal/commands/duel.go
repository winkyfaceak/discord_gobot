package commands

import (
	"fmt"
	"log"
	"maps"
	"math/rand/v2"
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

const (
	// duelWait is how long a challenge waits to be accepted, and then how
	// long rock-paper-scissors players have to pick (again after a draw).
	duelWait = 60 * time.Second
	// connect4Turn is how long a Connect 4 player has for each move.
	connect4Turn = 2 * time.Minute

	rockPaperScissors = "rock-paper-scissors"
	connect4          = "Connect 4"
)

// Rock, paper, scissors: each hand beats the one before it.
var rpsHands = [3]string{"🪨 Rock", "📄 Paper", "✂️ Scissors"}

// Duel implements /duel: challenge someone to rock-paper-scissors or
// Connect 4 for coins. Both stake the bet and the winner takes both stakes.
// A rock-paper-scissors draw is played again; a full Connect 4 board is
// refunded.
type Duel struct {
	casino *Casino

	mu    sync.Mutex
	duels map[string]*duel
}

type duel struct {
	id      string
	guildID snowflake.ID
	game    string
	players [2]discord.User // challenger, opponent
	bet     int64

	mu       sync.Mutex
	accepted bool   // the opponent's stake is in too
	done     bool   // paid out or refunded
	picks    [2]int // rock-paper-scissors: index into rpsHands, -1 until picked
	board    c4Board
	turn     int // Connect 4: whose move, as an index into players
	deadline time.Time
	timer    *time.Timer

	// The latest interaction that updated the message, whose token can
	// still edit it when the duel times out
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
		Description: "Challenge someone to rock-paper-scissors or Connect 4 for coins",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionUser{Name: "opponent", Description: "Who to challenge", Required: true},
			discord.ApplicationCommandOptionInt{Name: "bet", Description: "Coins each of you puts in", Required: true, MinValue: new(1)},
			discord.ApplicationCommandOptionString{
				Name:        "game",
				Description: "What to play. Defaults to rock-paper-scissors.",
				Choices: []discord.ApplicationCommandOptionChoiceString{
					{Name: "Rock paper scissors", Value: rockPaperScissors},
					{Name: "Connect 4", Value: connect4},
				},
			},
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
	game, ok := data.OptString("game")
	if !ok {
		game = rockPaperScissors
	}

	du := &duel{
		id:       randomID(),
		guildID:  *e.GuildID(),
		game:     game,
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
		du.turn = rand.IntN(2)
		du.restartClock()
		d.update(e, du, du.text(""), du.buttons())

	case "decline":
		if du.accepted {
			return
		}
		d.refundLocked(du)
		text := fmt.Sprintf("🏳️ <@%s> declined the duel. The bet was refunded.", du.players[1].ID)
		if seat == 0 {
			text = fmt.Sprintf("🏳️ <@%s> called off the duel. The bet was refunded.", du.players[0].ID)
		}
		d.update(e, du, text, nil)

	default:
		n, err := strconv.Atoi(action)
		if err != nil || !du.accepted {
			return
		}
		if du.game == connect4 {
			d.drop(e, du, seat, n)
		} else {
			d.pick(e, du, seat, n)
		}
	}
}

// pick is a rock-paper-scissors player choosing a hand.
func (d *Duel) pick(e *events.ComponentInteractionCreate, du *duel, seat, hand int) {
	if hand < 0 || hand >= len(rpsHands) {
		return
	}
	du.picks[seat] = hand
	other := du.picks[1-seat]
	switch {
	case other < 0:
		d.update(e, du, du.text(""), du.buttons())
		discordutil.FollowUp(e, fmt.Sprintf("You picked **%s**. Click another to change it.", rpsHands[hand]), true)
	case other == hand:
		du.picks = [2]int{-1, -1}
		du.restartClock()
		d.update(e, du, du.text(fmt.Sprintf("Both threw **%s**! Draw, go again.", rpsHands[hand])), du.buttons())
	default:
		winner := 0
		if rpsBeats(du.picks[1], du.picks[0]) {
			winner = 1
		}
		loser := 1 - winner
		d.finishLocked(du)
		d.casino.give(du.guildID, du.players[winner].ID, 2*du.bet)
		d.update(e, du, fmt.Sprintf("🏆 <@%s>'s **%s** beats <@%s>'s **%s**! <@%s> takes the **%s** coin pot.",
			du.players[winner].ID, rpsHands[du.picks[winner]], du.players[loser].ID, rpsHands[du.picks[loser]],
			du.players[winner].ID, formatInt(int(2*du.bet))), nil)
	}
}

// drop is a Connect 4 player dropping a piece into a column.
func (d *Duel) drop(e *events.ComponentInteractionCreate, du *duel, seat, col int) {
	if col < 0 || col >= c4Cols {
		return
	}
	if seat != du.turn {
		discordutil.Reply(e, fmt.Sprintf("It's %s's turn.", du.players[du.turn].EffectiveName()), true)
		return
	}
	row := du.board.drop(col, seat+1)
	switch {
	case row < 0:
		discordutil.Reply(e, "That column is full.", true)
	case du.board.wins(row, col):
		d.finishLocked(du)
		d.casino.give(du.guildID, du.players[seat].ID, 2*du.bet)
		d.update(e, du, fmt.Sprintf("🏆 %s <@%s> connects four and takes the **%s** coin pot!\n%s",
			c4Pieces[seat+1], du.players[seat].ID, formatInt(int(2*du.bet)), du.board.String()), nil)
	case du.board.full():
		d.refundLocked(du)
		d.update(e, du, "🤝 The board is full, so it's a draw. Both bets were refunded.\n"+du.board.String(), nil)
	default:
		du.turn = 1 - seat
		du.restartClock()
		d.update(e, du, du.text(""), du.buttons())
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

// expire ends a duel nobody finished in time: a Connect 4 player who runs
// out of time on their move loses, anything else is refunded.
func (d *Duel) expire(du *duel) {
	du.mu.Lock()
	defer du.mu.Unlock()
	if du.done || time.Now().Before(du.deadline) { // finished, or the clock restarted while this waited
		return
	}
	var text string
	switch {
	case !du.accepted:
		d.refundLocked(du)
		text = fmt.Sprintf("⌛ <@%s> didn't accept in time. The bet was refunded.", du.players[1].ID)
	case du.game == connect4:
		winner := 1 - du.turn
		d.finishLocked(du)
		d.casino.give(du.guildID, du.players[winner].ID, 2*du.bet)
		text = fmt.Sprintf("⌛ <@%s> ran out of time, so <@%s> takes the **%s** coin pot.\n%s",
			du.players[du.turn].ID, du.players[winner].ID, formatInt(int(2*du.bet)), du.board.String())
	default:
		d.refundLocked(du)
		text = "⌛ The duel timed out before both players picked. Both bets were refunded."
	}
	if _, err := discordutil.EditOriginal(du, discord.MessageUpdate{Content: &text, Components: &[]discord.LayoutComponent{}}); err != nil {
		log.Printf("duel: expire: %v", err)
	}
}

// update answers a button press by editing the duel message, and keeps the
// press's token for edits after a timeout (the /duel token only lasts 15
// minutes, a long Connect 4 game doesn't).
func (d *Duel) update(e *events.ComponentInteractionCreate, du *duel, text string, components []discord.LayoutComponent) {
	if components == nil {
		components = []discord.LayoutComponent{} // an empty list removes the buttons
	}
	if err := e.UpdateMessage(discord.MessageUpdate{Content: &text, Components: &components}); err != nil {
		log.Printf("duel: update: %v", err)
		return
	}
	du.client, du.appID, du.token = e.Client(), e.ApplicationID(), e.Token()
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
	wait := duelWait
	if du.game == connect4 {
		wait = connect4Turn
	}
	du.deadline = time.Now().Add(wait)
	du.timer.Reset(wait)
}

// text shows the challenge, then the game in progress. note goes after the
// rock-paper-scissors matchup line.
func (du *duel) text(note string) string {
	a, b := du.players[0].ID, du.players[1].ID
	if !du.accepted {
		return fmt.Sprintf("⚔️ <@%s> challenges <@%s> to %s for **%s** coins each, winner takes both. The challenge ends <t:%d:R>.",
			a, b, du.game, formatInt(int(du.bet)), du.deadline.Unix())
	}
	if du.game == connect4 {
		return fmt.Sprintf("⚔️ %s <@%s> vs %s <@%s> for a **%s** coin pot.\n%s%s <@%s>'s turn, time runs out <t:%d:R>.",
			c4Pieces[1], a, c4Pieces[2], b, formatInt(int(2*du.bet)), du.board.String(),
			c4Pieces[du.turn+1], du.players[du.turn].ID, du.deadline.Unix())
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
	if du.game == connect4 {
		// Seven columns, in rows of 4 and 3 (a row holds at most 5 buttons)
		var cols []discord.InteractiveComponent
		for col := range c4Cols {
			cols = append(cols, discord.NewSecondaryButton(strconv.Itoa(col+1), prefix+strconv.Itoa(col)).WithDisabled(du.board[0][col] != 0))
		}
		return []discord.LayoutComponent{discord.NewActionRow(cols[:4]...), discord.NewActionRow(cols[4:]...)}
	}
	var hands []discord.InteractiveComponent
	for i, name := range rpsHands {
		hands = append(hands, discord.NewSecondaryButton(name, prefix+strconv.Itoa(i)))
	}
	return []discord.LayoutComponent{discord.NewActionRow(hands...)}
}

// rpsBeats reports whether hand a beats hand b.
func rpsBeats(a, b int) bool { return (a-b+3)%3 == 1 }
