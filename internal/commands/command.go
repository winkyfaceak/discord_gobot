package commands

import (
	deadlockapi "discord_gobot/internal/deadlock"

	"github.com/bwmarrin/discordgo"
)

// Command is the interface every slash command must implement.
//
// Each command has two jobs:
//
//  1. Definition()
//     Tells Discord what the command looks like.
//     This includes name, description, and options.
//
//  2. Handle()
//     Runs when the user actually executes the command.
type Command interface {
	Definition() *discordgo.ApplicationCommand
	Handle(s *discordgo.Session, i *discordgo.InteractionCreate)
}

// ComponentCommand is implemented by commands that own Discord message components
// such as buttons or select menus. The bot routes component interactions by prefix.
type ComponentCommand interface {
	ComponentPrefix() string
	HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate)
}

// ShutdownCommand releases long-running command resources when the bot stops.
type ShutdownCommand interface {
	Shutdown()
}

// All returns the complete command list for the bot.
//
// To add a new command:
//  1. Create a new file in internal/commands.
//  2. Make a struct that implements Command.
//  3. Add it to this slice.
func All() []Command {
	deadlockService := deadlockapi.NewService(deadlockapi.NewClient(nil))

	return []Command{
		Ping{},
		Hello{},
		Echo{},
		Add{},
		Weather{},
		NewDeadlockStatistics(deadlockService),
		NewServerStats(nil, nil),
	}
}
