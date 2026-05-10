package commands

import "github.com/bwmarrin/discordgo"

// Command is the interface every slash command must implement
//
// Each command has two jobs:
//
//  1. Definition()
//     Tells Discord what the command looks like
//     This includes name, description, and options
//
//  2. Handle()
//     Runs when the user actually executes the command
type Command interface {
	Definition() *discordgo.ApplicationCommand
	Handle(s *discordgo.Session, i *discordgo.InteractionCreate)
}

// All returns the complete command list for the bot
//
// To add a new command:
//  1. Create a new file in internal/commands
//  2. Make a struct that implements Command
//  3. Add it to this slice
func All() []Command {
	return []Command{
		Ping{},
		Hello{},
		Echo{},
		Add{},
		Weather{},
	}
}
