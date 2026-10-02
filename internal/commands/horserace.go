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
	raceHorses = 5
	raceFinish = 15 // steps from the gate to the line
	raceWait   = 60 * time.Second
	raceTick   = 1500 * time.Millisecond // gentle on Discord's edit rate limit
	minRiders  = 2
)

var (
	raceHorseNames = [raceHorses]string{"Thunder", "Biscuit", "Sir Gallops", "Lucky Dip", "Glue Factory"}
	raceColors     = [raceHorses]string{"🟥", "🟧", "🟨", "🟩", "🟦"}
)

// HorseRace implements /horserace: an open race everyone can bet on for a
// minute, then a live race. Everyone stakes the same amount, and whoever
// backed the winner splits the pot. One race per channel at a time.
type HorseRace struct {
	casino *Casino

	mu    sync.Mutex
	races map[string]*race
}

type race struct {
	id        string
	guildID   snowflake.ID
	channelID snowflake.ID
	stake     int64
	starts    time.Time

	// The command's interaction, whose token edits the race message
	client *bot.Client
	appID  snowflake.ID
	token  string

	mu    sync.Mutex
	open  bool // still taking bets
	done  bool // paid out or refunded
	bets  map[snowflake.ID]int
	names map[snowflake.ID]string
}

func (r *race) Client() *bot.Client         { return r.client }
func (r *race) ApplicationID() snowflake.ID { return r.appID }
func (r *race) Token() string               { return r.token }

func NewHorseRace(casino *Casino) *HorseRace {
	return &HorseRace{casino: casino, races: make(map[string]*race)}
}

func (h *HorseRace) ComponentPrefix() string { return "horserace" }

func (h *HorseRace) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "horserace",
		Description: "Open a horse race everyone can bet on",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionInt{Name: "stake", Description: "Coins each rider bets", Required: true, MinValue: new(1)},
		},
	}
}

func (h *HorseRace) Handle(e *events.ApplicationCommandInteractionCreate) {
	r := &race{
		id:        randomID(),
		guildID:   *e.GuildID(),
		channelID: e.Channel().ID(),
		stake:     int64(e.SlashCommandInteractionData().Int("stake")),
		starts:    time.Now().Add(raceWait),
		client:    e.Client(),
		appID:     e.ApplicationID(),
		token:     e.Token(),
		open:      true,
		bets:      make(map[snowflake.ID]int),
		names:     make(map[snowflake.ID]string),
	}

	h.mu.Lock()
	for _, other := range h.races {
		if other.channelID == r.channelID {
			h.mu.Unlock()
			discordutil.Reply(e, "There's already a race in this channel. Wait for it to finish.", true)
			return
		}
	}
	h.races[r.id] = r
	h.mu.Unlock()

	r.mu.Lock()
	content := r.bettingText()
	r.mu.Unlock()
	if err := e.CreateMessage(discord.MessageCreate{Content: content, Components: raceButtons(r.id)}); err != nil {
		log.Printf("horserace: open: %v", err)
		h.remove(r)
		return
	}
	time.AfterFunc(raceWait, func() { h.run(r) })
}

func (h *HorseRace) HandleComponent(e *events.ComponentInteractionCreate) {
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) != 3 {
		return
	}
	h.mu.Lock()
	r := h.races[parts[1]]
	h.mu.Unlock()
	horse, err := strconv.Atoi(parts[2])
	if r == nil || err != nil || horse < 0 || horse >= raceHorses {
		discordutil.Reply(e, "That race is over. Start a new one with `/horserace`.", true)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	user := e.User()
	switch current, riding := r.bets[user.ID]; {
	case !r.open:
		discordutil.Reply(e, "Betting has closed for this race.", true)
		return
	case riding && current == horse:
		discordutil.Reply(e, fmt.Sprintf("You're already on **%s**.", raceHorseNames[horse]), true)
		return
	case !riding:
		if _, problem := h.casino.take(r.guildID, user.ID, r.stake); problem != "" {
			discordutil.Reply(e, problem, true)
			return
		}
	}
	// New riders pay the stake; existing ones just switch horse
	r.bets[user.ID] = horse
	r.names[user.ID] = user.EffectiveName()

	content := r.bettingText()
	components := raceButtons(r.id)
	if err := e.UpdateMessage(discord.MessageUpdate{Content: &content, Components: &components}); err != nil {
		log.Printf("horserace: update bets: %v", err)
	}
}

// Shutdown refunds races that haven't finished.
func (h *HorseRace) Shutdown() {
	h.mu.Lock()
	open := slices.Collect(maps.Values(h.races))
	h.mu.Unlock()
	for _, r := range open {
		r.mu.Lock()
		h.refundLocked(r)
		r.mu.Unlock()
	}
}

// run closes betting, then either refunds (too few riders) or races live and pays out.
func (h *HorseRace) run(r *race) {
	defer h.remove(r)

	r.mu.Lock()
	r.open = false
	if len(r.bets) < minRiders {
		h.refundLocked(r)
		r.mu.Unlock()
		text := fmt.Sprintf("🏇 Not enough riders for this race (it needs %d), so every stake was refunded.", minRiders)
		h.edit(r, text)
		return
	}
	bets := maps.Clone(r.bets)
	pot := r.stake * int64(len(bets))
	r.mu.Unlock()

	ticks, winner := runRace(rand.IntN)
	for i, positions := range ticks {
		header := fmt.Sprintf("🏇 **And they're off!** Pot: **%s** coins\n", formatInt(int(pot)))
		if i == len(ticks)-1 {
			header = fmt.Sprintf("🏇 **%s wins!**\n", raceHorseNames[winner])
		}
		h.edit(r, header+raceLanes(positions, i == len(ticks)-1, winner))
		time.Sleep(raceTick)
	}

	r.mu.Lock()
	r.done = true
	r.mu.Unlock()

	winnings := raceWinnings(bets, winner, pot)
	var result string
	if len(winnings) == 0 {
		result = fmt.Sprintf("Nobody backed **%s**, so the house keeps the **%s** coin pot.", raceHorseNames[winner], formatInt(int(pot)))
	} else {
		var winners []string
		for userID, coins := range winnings {
			h.casino.give(r.guildID, userID, coins)
			winners = append(winners, fmt.Sprintf("<@%s> (+%s)", userID, formatInt(int(coins-r.stake))))
		}
		slices.Sort(winners)
		result = fmt.Sprintf("Winners split the **%s** coin pot: %s", formatInt(int(pot)), strings.Join(winners, ", "))
	}
	final := fmt.Sprintf("🏆 **%s wins!**\n%s\n%s", raceHorseNames[winner], raceLanes(ticks[len(ticks)-1], true, winner), result)
	h.edit(r, final)
}

func (h *HorseRace) edit(r *race, text string) {
	if _, err := discordutil.EditOriginal(r, discord.MessageUpdate{Content: &text, Components: &[]discord.LayoutComponent{}}); err != nil {
		log.Printf("horserace: update race: %v", err)
	}
}

func (h *HorseRace) refundLocked(r *race) {
	if r.done {
		return
	}
	for userID := range r.bets {
		h.casino.give(r.guildID, userID, r.stake)
	}
	r.done = true
}

func (h *HorseRace) remove(r *race) {
	h.mu.Lock()
	delete(h.races, r.id)
	h.mu.Unlock()
}

// bettingText shows the open race: stake, start time, and who's on which horse.
func (r *race) bettingText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "🏇 **Horse race!** Stake: **%s** coins a rider. The gates open <t:%d:R>.\n", formatInt(int(r.stake)), r.starts.Unix())
	for horse, name := range raceHorseNames {
		var riders []string
		for userID, h := range r.bets {
			if h == horse {
				riders = append(riders, r.names[userID])
			}
		}
		slices.Sort(riders)
		fmt.Fprintf(&b, "%s **%d %s**", raceColors[horse], horse+1, name)
		if len(riders) > 0 {
			b.WriteString(": " + strings.Join(riders, ", "))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Pot: **%s** coins from %d rider(s). Needs at least %d. Pick a horse below; click another to switch.",
		formatInt(int(r.stake)*len(r.bets)), len(r.bets), minRiders)
	return b.String()
}

func raceButtons(raceID string) []discord.LayoutComponent {
	var buttons []discord.InteractiveComponent
	for horse, name := range raceHorseNames {
		buttons = append(buttons, discord.NewSecondaryButton(fmt.Sprintf("%d %s", horse+1, name), fmt.Sprintf("horserace:%s:%d", raceID, horse)))
	}
	return []discord.LayoutComponent{discord.NewActionRow(buttons...)}
}

// runRace moves every horse 0 to 3 steps a tick until one reaches the
// finish, returning the positions after each tick and the winner. If several
// cross on the same tick, the furthest wins, then a photo finish (roll).
// Every horse has the same chance.
func runRace(roll func(n int) int) (ticks [][raceHorses]int, winner int) {
	var positions [raceHorses]int
	for {
		for horse := range positions {
			positions[horse] += roll(4)
		}
		ticks = append(ticks, positions)

		best, leaders := -1, []int(nil)
		for horse, p := range positions {
			switch {
			case p > best:
				best, leaders = p, []int{horse}
			case p == best:
				leaders = append(leaders, horse)
			}
		}
		if best >= raceFinish {
			return ticks, leaders[roll(len(leaders))]
		}
	}
}

// raceLanes draws the track. 🏇 faces left, so horses run right to left
// towards the flag.
func raceLanes(positions [raceHorses]int, finished bool, winner int) string {
	var b strings.Builder
	for horse, p := range positions {
		p = min(p, raceFinish)
		fmt.Fprintf(&b, "%s 🏁%s🏇%s **%d** %s", raceColors[horse], strings.Repeat("·", raceFinish-p), strings.Repeat("·", p), horse+1, raceHorseNames[horse])
		if finished && horse == winner {
			b.WriteString(" 🏆")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// raceWinnings splits the pot evenly between everyone who backed the winner.
func raceWinnings(bets map[snowflake.ID]int, winner int, pot int64) map[snowflake.ID]int64 {
	var backers []snowflake.ID
	for userID, horse := range bets {
		if horse == winner {
			backers = append(backers, userID)
		}
	}
	winnings := make(map[snowflake.ID]int64, len(backers))
	for _, userID := range backers {
		winnings[userID] = pot / int64(len(backers))
	}
	return winnings
}
