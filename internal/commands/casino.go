package commands

import (
	"cmp"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

const (
	startingCoins = 1000
	dailyCoins    = 200
	dailyCooldown = 20 * time.Hour // "once a day" without drifting later each day
)

// Casino is a fake-coin economy, separate per server: /coinflip, /slots,
// /daily, /balance and /leaderboard. Coins have no real-world value.
type Casino struct {
	path string

	mu       sync.Mutex
	accounts map[snowflake.ID]map[snowflake.ID]*account // server → member
}

type account struct {
	Coins     int64     `json:"coins"`
	LastDaily time.Time `json:"last_daily"`
}

func NewCasino(path string) *Casino {
	c := &Casino{path: path, accounts: make(map[snowflake.ID]map[snowflake.ID]*account)}
	if err := loadJSON(path, &c.accounts); err != nil {
		log.Printf("casino: load %s: %v", path, err)
	}
	return c
}

func (c *Casino) Commands() []Command {
	guildOnly := []discord.InteractionContextType{discord.InteractionContextTypeGuild}
	bet := discord.ApplicationCommandOptionInt{Name: "bet", Description: "How many coins to bet", Required: true, MinValue: new(1)}

	return []Command{
		funcCommand{
			def: discord.SlashCommandCreate{
				Name:        "coinflip",
				Description: "Bet coins on heads or tails: double or nothing",
				Contexts:    guildOnly,
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:        "call",
						Description: "Heads or tails?",
						Required:    true,
						Choices: []discord.ApplicationCommandOptionChoiceString{
							{Name: "Heads", Value: "heads"},
							{Name: "Tails", Value: "tails"},
						},
					},
					bet,
				},
			},
			handle: c.coinflip,
		},
		NewSlots(c),
		c.rouletteCommand(),
		NewBlackjack(c),
		NewHorseRace(c),
		funcCommand{
			def:    discord.SlashCommandCreate{Name: "daily", Description: fmt.Sprintf("Claim %d free coins once a day", dailyCoins), Contexts: guildOnly},
			handle: c.daily,
		},
		funcCommand{
			def: discord.SlashCommandCreate{
				Name:        "balance",
				Description: "How many coins you (or someone else) have",
				Contexts:    guildOnly,
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionUser{Name: "who", Description: "Whose balance. Defaults to you."},
				},
			},
			handle: c.balance,
		},
		funcCommand{
			def:    discord.SlashCommandCreate{Name: "leaderboard", Description: "The richest members of this server", Contexts: guildOnly},
			handle: c.leaderboard,
		},
	}
}

func (c *Casino) coinflip(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()
	call, bet := data.String("call"), int64(data.Int("bet"))

	landed := "heads"
	if rand.IntN(2) == 1 {
		landed = "tails"
	}
	balance, problem := c.play(*e.GuildID(), e.User().ID, bet, func() int64 {
		if landed == call {
			return 2 * bet
		}
		return 0
	})
	if problem != "" {
		discordutil.Reply(e, problem, true)
		return
	}

	result := "lost **" + formatInt(int(bet)) + "** coins"
	if landed == call {
		result = "won **" + formatInt(int(bet)) + "** coins"
	}
	discordutil.Reply(e, fmt.Sprintf("🪙 %s called **%s**… it landed on **%s**! You %s. Balance: **%s**",
		e.User().EffectiveName(), call, landed, result, formatInt(int(balance))), false)
}

func (c *Casino) daily(e *events.ApplicationCommandInteractionCreate) {
	c.mu.Lock()
	acct := c.accountLocked(*e.GuildID(), e.User().ID)
	next := acct.LastDaily.Add(dailyCooldown)
	claimed := time.Now().After(next)
	if claimed {
		acct.Coins += dailyCoins
		acct.LastDaily = time.Now()
		c.saveLocked()
	}
	balance := acct.Coins
	c.mu.Unlock()

	if !claimed {
		discordutil.Reply(e, fmt.Sprintf("You've had today's coins. Come back <t:%d:R>.", next.Unix()), true)
		return
	}
	discordutil.Reply(e, fmt.Sprintf("💰 %s claimed **%d** daily coins. Balance: **%s**", e.User().EffectiveName(), dailyCoins, formatInt(int(balance))), false)
}

func (c *Casino) balance(e *events.ApplicationCommandInteractionCreate) {
	who, ok := e.SlashCommandInteractionData().OptSnowflake("who")
	if !ok {
		who = e.User().ID
	}

	discordutil.Reply(e, fmt.Sprintf("<@%s> has **%s** coins.", who, formatInt(int(c.coins(*e.GuildID(), who)))), false)
}

func (c *Casino) leaderboard(e *events.ApplicationCommandInteractionCreate) {
	type entry struct {
		id    snowflake.ID
		coins int64
	}
	c.mu.Lock()
	var entries []entry
	for id, acct := range c.accounts[*e.GuildID()] {
		entries = append(entries, entry{id, acct.Coins})
	}
	c.mu.Unlock()

	if len(entries) == 0 {
		discordutil.Reply(e, "Nobody here has played yet. Try `/coinflip` or `/slots`.", false)
		return
	}
	slices.SortFunc(entries, func(a, b entry) int { return cmp.Compare(b.coins, a.coins) })

	var board strings.Builder
	board.WriteString("🏆 **Richest in this server**\n")
	for i, en := range entries[:min(len(entries), 10)] {
		fmt.Fprintf(&board, "%d. <@%s> **%s** coins\n", i+1, en.id, formatInt(int(en.coins)))
	}
	discordutil.Reply(e, board.String(), false)
}

// play takes a bet, runs the game for its payout (0 = lost) and saves the
// new balance. problem explains why the bet couldn't be placed.
func (c *Casino) play(guildID, userID snowflake.ID, bet int64, game func() int64) (balance int64, problem string) {
	if _, problem := c.take(guildID, userID, bet); problem != "" {
		return 0, problem
	}
	return c.give(guildID, userID, game()), ""
}

// take removes coins for a bet, or explains why it can't.
func (c *Casino) take(guildID, userID snowflake.ID, amount int64) (balance int64, problem string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	acct := c.accountLocked(guildID, userID)
	if amount > acct.Coins {
		return acct.Coins, fmt.Sprintf("You only have **%s** coins. `/daily` gives you more.", formatInt(int(acct.Coins)))
	}
	acct.Coins -= amount
	c.saveLocked()
	return acct.Coins, ""
}

// give adds coins (winnings or a refund) and returns the new balance.
func (c *Casino) give(guildID, userID snowflake.ID, amount int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	acct := c.accountLocked(guildID, userID)
	acct.Coins += amount
	if amount != 0 {
		c.saveLocked()
	}
	return acct.Coins
}

// coins is a member's current balance.
func (c *Casino) coins(guildID, userID snowflake.ID) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accountLocked(guildID, userID).Coins
}

// accountLocked returns a member's account, opening it with starting coins.
func (c *Casino) accountLocked(guildID, userID snowflake.ID) *account {
	if c.accounts[guildID] == nil {
		c.accounts[guildID] = make(map[snowflake.ID]*account)
	}
	acct := c.accounts[guildID][userID]
	if acct == nil {
		acct = &account{Coins: startingCoins}
		c.accounts[guildID][userID] = acct
	}
	return acct
}

func (c *Casino) saveLocked() {
	if err := saveJSON(c.path, c.accounts); err != nil {
		log.Printf("casino: save: %v", err)
	}
}
