package commands

import (
	"fmt"

	"discord_gobot/internal/discordutil"

	"github.com/bwmarrin/discordgo"
)

// Add implements the /add command
//
// Example:
//
//	/add a:2 b:3
type Add struct{}

// Definition tells Discord this command requires two integer options
func (Add) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "add",
		Description: "Add two numbers together",

		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "a",
				Description: "The first number",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "b",
				Description: "The second number",
				Required:    true,
			},
		},
	}
}

// Handle runs when the user executes /add
func (Add) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := discordutil.OptionMap(i.ApplicationCommandData().Options)

	aOption, ok := options["a"]
	if !ok {
		discordutil.Respond(s, i, "Missing required input: a", true)
		return
	}

	bOption, ok := options["b"]
	if !ok {
		discordutil.Respond(s, i, "Missing required input: b", true)
		return
	}

	// IntValue reads the integer value from the slash command option
	a := aOption.IntValue()
	b := bOption.IntValue()

	discordutil.Respond(
		s,
		i,
		fmt.Sprintf("%d + %d = %d", a, b, a+b),
		false,
	)
}
