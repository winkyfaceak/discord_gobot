package commands

import (
	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

// Ping implements the /ping command
//
// It is useful as the simplest possible test command
type Ping struct{}

// Definition tells Discord how the /ping command should appear
func (Ping) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "ping",
		Description: "Check whether the bot is alive",
	}
}

// Handle runs when the user executes /ping
func (Ping) Handle(e *events.ApplicationCommandInteractionCreate) {
	discordutil.Reply(e, "Pong!", false)
}
