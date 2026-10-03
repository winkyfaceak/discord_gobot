package commands

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"
	"discord_gobot/internal/slots"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// Slots implements /slots: an animated slot machine. The result message has
// a Spin again button (same bet) that respins in place.
type Slots struct {
	casino *Casino

	mu       sync.Mutex
	spinning map[snowflake.ID]bool // messages mid-spin, so double clicks don't overlap
}

func NewSlots(casino *Casino) *Slots {
	return &Slots{casino: casino, spinning: make(map[snowflake.ID]bool)}
}

func (s *Slots) ComponentPrefix() string { return "slots" }

func (s *Slots) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "slots",
		Description: "Spin the slot machine",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionInt{Name: "bet", Description: "How many coins to bet", Required: true, MinValue: new(1)},
		},
	}
}

func (s *Slots) Handle(e *events.ApplicationCommandInteractionCreate) {
	bet := int64(e.SlashCommandInteractionData().Int("bet"))
	reels, balance, problem := s.play(*e.GuildID(), e.User().ID, bet)
	if problem != "" {
		discordutil.Reply(e, problem, true)
		return
	}
	if err := e.DeferCreateMessage(false); err != nil {
		log.Printf("slots: defer: %v", err)
		return
	}
	s.show(e, e.User(), bet, reels, balance)
}

// HandleComponent handles Spin again: "slots:again:<player>:<bet>".
func (s *Slots) HandleComponent(e *events.ComponentInteractionCreate) {
	parts := strings.Split(e.Data.CustomID(), ":")
	if len(parts) != 4 || parts[1] != "again" {
		return
	}
	bet, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return
	}
	if parts[2] != e.User().ID.String() {
		discordutil.Reply(e, "This is someone else's machine. Spin your own with `/slots`.", true)
		return
	}

	messageID := e.Message.ID
	s.mu.Lock()
	busy := s.spinning[messageID]
	s.spinning[messageID] = true
	s.mu.Unlock()
	if busy {
		discordutil.Reply(e, "Still spinning!", true)
		return
	}
	defer func() {
		s.mu.Lock()
		delete(s.spinning, messageID)
		s.mu.Unlock()
	}()

	reels, balance, problem := s.play(*e.GuildID(), e.User().ID, bet)
	if problem != "" {
		discordutil.Reply(e, problem, true)
		return
	}
	if err := e.DeferUpdateMessage(); err != nil {
		log.Printf("slots: defer spin again: %v", err)
		return
	}
	s.show(e, e.User(), bet, reels, balance)
}

// play spins and settles the bet straight away; the animation just reveals it.
func (s *Slots) play(guildID, userID snowflake.ID, bet int64) (reels [3]int, balance int64, problem string) {
	reels = slots.Spin()
	multiplier := slots.Multiplier(reels)
	balance, problem = s.casino.play(guildID, userID, bet, func() int64 { return bet * multiplier })
	return reels, balance, problem
}

// show posts the spinning reels, then the result with a Spin again button.
func (s *Slots) show(i discordutil.Interaction, player discord.User, bet int64, reels [3]int, balance int64) {
	name := player.EffectiveName()
	multiplier := slots.Multiplier(reels)
	var result string
	switch {
	case multiplier == 0:
		result = "lost **" + formatInt(int(bet)) + "** coins"
	case multiplier == 1:
		result = "got the bet back"
	default:
		result = "won **" + formatInt(int(bet*(multiplier-1))) + "** coins"
	}
	final := fmt.Sprintf("🎰 **%s** bet **%s**: %s %s %s. %s, %s. Balance: **%s**",
		name, formatInt(int(bet)), slots.Symbols[reels[0]].Emoji, slots.Symbols[reels[1]].Emoji, slots.Symbols[reels[2]].Emoji,
		slots.Describe(reels), result, formatInt(int(balance)))
	again := []discord.LayoutComponent{discord.NewActionRow(
		discord.NewPrimaryButton(fmt.Sprintf("Spin again (%s)", formatInt(int(bet))), fmt.Sprintf("slots:again:%s:%d", player.ID, bet)),
	)}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	spin, still, err := slots.RenderSpin(ctx, reels)
	if err != nil {
		log.Printf("slots: render: %v", err)
		if _, err := discordutil.EditOriginal(i, discord.MessageUpdate{Content: &final, Components: &again}); err != nil {
			log.Printf("slots: post result: %v", err)
		}
		return
	}

	spinning := fmt.Sprintf("🎰 **%s** spins for **%s**…", name, formatInt(int(bet)))
	update := discordutil.ImageUpdate("slots.gif", "Slot machine reels spinning", spin, discord.Embed{Color: 0xa52335}, nil)
	update.Content = &spinning
	if _, err := discordutil.EditOriginal(i, update); err != nil {
		log.Printf("slots: post spin: %v", err)
		return
	}

	// Let the reels stop (plus time for the GIF to load), then show the result
	time.Sleep(slots.SpinDuration + 1200*time.Millisecond)
	update = discordutil.ImageUpdate("slots-result.png", "Slot machine result: "+slots.Describe(reels), still, discord.Embed{Color: 0xa52335}, again)
	update.Content = &final
	if _, err := discordutil.EditOriginal(i, update); err != nil {
		log.Printf("slots: post result: %v", err)
	}
}
