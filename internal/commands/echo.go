package commands

import (
	"discord_gobot/internal/discordutil"

	"github.com/bwmarrin/discordgo"
)

// Echo implements the /echo command
//
// Example:
//
//	/echo text:"hello world"
//	/echo text:"secret message" private:true
type Echo struct{}

// Definition describes the command and its options to Discord
func (Echo) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "echo",
		Description: "Repeat some text back to you",

		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "text",
				Description: "The text to repeat",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "private",
				Description: "Only show the response to you",
				Required:    false,
			},
		},
	}
}

// Handle runs when the user executes /echo.
func (Echo) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := discordutil.OptionMap(i.ApplicationCommandData().Options)

	textOption, ok := options["text"]
	if !ok {
		discordutil.Respond(s, i, "Missing required input: text", true)
		return
	}

	// Optional options may not be present
	//
	// So we start with a default value, then override it only if the user
	// provided the option
	private := false

	if privateOption, ok := options["private"]; ok {
		private = privateOption.BoolValue()
	}

	discordutil.Respond(
		s,
		i,
		textOption.StringValue(),
		private,
	)
}
