package commands

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"time"

	"discord_gobot/internal/discordutil"
	"discord_gobot/internal/roulette"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

var rouletteBets = []discord.ApplicationCommandOptionChoiceString{
	{Name: "Red", Value: "red"},
	{Name: "Black", Value: "black"},
	{Name: "Odd", Value: "odd"},
	{Name: "Even", Value: "even"},
	{Name: "Low (1–18)", Value: "low"},
	{Name: "High (19–36)", Value: "high"},
	{Name: "1st dozen (1–12)", Value: "dozen1"},
	{Name: "2nd dozen (13–24)", Value: "dozen2"},
	{Name: "3rd dozen (25–36)", Value: "dozen3"},
	{Name: "Single number (set number)", Value: "number"},
}

func (c *Casino) rouletteCommand() Command {
	return funcCommand{
		def: discord.SlashCommandCreate{
			Name:        "roulette",
			Description: "Spin the roulette wheel",
			Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{Name: "on", Description: "What to bet on", Required: true, Choices: rouletteBets},
				discord.ApplicationCommandOptionInt{Name: "bet", Description: "How many coins to bet", Required: true, MinValue: new(1)},
				discord.ApplicationCommandOptionInt{Name: "number", Description: "For a single number bet: 0 to 36", MinValue: new(0), MaxValue: new(36)},
			},
		},
		handle: c.roulette,
	}
}

func (c *Casino) roulette(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()
	on, bet := data.String("on"), int64(data.Int("bet"))
	number, hasNumber := data.OptInt("number")
	if on == "number" && !hasNumber {
		discordutil.Reply(e, "Pick your number (0 to 36) with the `number` option.", true)
		return
	}
	if on != "number" && hasNumber {
		discordutil.Reply(e, "The `number` option is only for a **Single number** bet.", true)
		return
	}

	result := rand.IntN(37)
	multiplier := rouletteMultiplier(on, number, result)
	balance, problem := c.play(*e.GuildID(), e.User().ID, bet, func() int64 { return bet * multiplier })
	if problem != "" {
		discordutil.Reply(e, problem, true)
		return
	}
	if err := e.DeferCreateMessage(false); err != nil {
		log.Printf("roulette: defer: %v", err)
		return
	}

	name := e.User().EffectiveName()
	label := rouletteLabel(on, number)
	outcome := "lost **" + formatInt(int(bet)) + "** coins"
	if multiplier > 0 {
		outcome = fmt.Sprintf("won **%s** coins (×%d)", formatInt(int(bet*(multiplier-1))), multiplier)
	}
	final := fmt.Sprintf("🎡 **%s** bet **%s** on **%s**. The ball landed on **%d %s**: %s! Balance: **%s**",
		name, formatInt(int(bet)), label, result, colorEmoji(result), outcome, formatInt(int(balance)))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	spin, still, err := roulette.RenderSpin(ctx, result)
	if err != nil {
		log.Printf("roulette: render: %v", err)
		discordutil.EditText(e, final) // the bet is settled either way
		return
	}

	spinning := fmt.Sprintf("🎡 **%s** bets **%s** on **%s**… no more bets!", name, formatInt(int(bet)), label)
	update := discordutil.ImageUpdate("roulette.gif", "Roulette wheel spinning", spin, discord.Embed{Color: 0xc9a227}, nil)
	update.Content = &spinning
	if _, err := discordutil.EditOriginal(e, update); err != nil {
		log.Printf("roulette: post spin: %v", err)
		discordutil.EditText(e, final)
		return
	}

	// Let the GIF play out (plus time for it to load), then show the result
	time.Sleep(roulette.SpinDuration + 1500*time.Millisecond)
	update = discordutil.ImageUpdate("roulette-result.png", fmt.Sprintf("Roulette wheel stopped on %d", result), still,
		discord.Embed{Color: map[string]int{"red": 0xb3262e, "black": 0x1c1c1c, "green": 0x1f7a3a}[roulette.Color(result)]}, nil)
	update.Content = &final
	if _, err := discordutil.EditOriginal(e, update); err != nil {
		log.Printf("roulette: post result: %v", err)
	}
}

// rouletteMultiplier is what a bet on `on` pays per coin when the ball lands
// on result: 36 for the right single number, 3 for the right dozen, 2 for
// the other bets, otherwise 0. Zero only pays a bet on 0 itself.
func rouletteMultiplier(on string, number, result int) int64 {
	if on == "number" {
		if result == number {
			return 36
		}
		return 0
	}
	if result == 0 {
		return 0
	}

	won, pays := false, int64(2)
	switch on {
	case "red", "black":
		won = roulette.Color(result) == on
	case "odd":
		won = result%2 == 1
	case "even":
		won = result%2 == 0
	case "low":
		won = result <= 18
	case "high":
		won = result >= 19
	case "dozen1", "dozen2", "dozen3":
		won, pays = (result-1)/12 == int(on[5]-'1'), 3
	}
	if won {
		return pays
	}
	return 0
}

func rouletteLabel(on string, number int) string {
	if on == "number" {
		return fmt.Sprintf("%d %s", number, colorEmoji(number))
	}
	for _, choice := range rouletteBets {
		if choice.Value == on {
			return choice.Name
		}
	}
	return on
}

func colorEmoji(n int) string {
	return map[string]string{"red": "🔴", "black": "⚫", "green": "🟢"}[roulette.Color(n)]
}
