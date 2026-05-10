package commands

import (
	"discord_gobot/internal/discordutil"

	"github.com/bwmarrin/discordgo"
)

// Ping implements the /ping command
//
// It is useful as the simplest possible test command
type Ping struct{}

// Definition tells Discord how the /ping command should appear
//
// This command has no options, so the user simply runs:
//
//	/ping
func (Ping) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "ping",
		Description: "Check whether the bot is alive",
	}
}

// Handle runs when the user executes /ping
func (Ping) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	discordutil.Respond(s, i, "Pong!", false)
}
