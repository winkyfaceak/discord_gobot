package commands

import (
	"fmt"

	"discord_gobot/internal/discordutil"

	"github.com/bwmarrin/discordgo"
)

// Hello implements the /hello command
//
// Example:
//
//	/hello name:James
type Hello struct{}

// Definition tells Discord the command name, description, and expected inputs
func (Hello) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "hello",
		Description: "Say hello to someone",

		// Options are the inputs Discord shows in the slash command UI
		Options: []*discordgo.ApplicationCommandOption{
			{
				// This option accepts text
				Type: discordgo.ApplicationCommandOptionString,

				// The user sees this as:
				//
				//   /hello name:
				Name: "name",

				Description: "The name to say hello to",

				// Required means Discord will not let the command run
				// unless the user provides this option
				Required: true,
			},
		},
	}
}

// Handle runs when the user executes /hello.
func (Hello) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Convert the options slice into a map so we can look up options by name
	options := discordutil.OptionMap(i.ApplicationCommandData().Options)

	nameOption, ok := options["name"]
	if !ok {
		// This should almost never happen because the option is required,
		// but defensive checks prevent panics
		discordutil.Respond(s, i, "Missing required input: name", true)
		return
	}

	name := nameOption.StringValue()

	discordutil.Respond(
		s,
		i,
		fmt.Sprintf("Hello, %s!", name),
		false,
	)
}
