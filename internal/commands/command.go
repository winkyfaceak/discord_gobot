package commands

import (
	"discord_gobot/internal/config"
	deadlockapi "discord_gobot/internal/deadlock"
	"discord_gobot/internal/navidrome"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
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
	Definition() discord.SlashCommandCreate
	Handle(e *events.ApplicationCommandInteractionCreate)
}

// ComponentCommand is implemented by commands that own Discord message components
// such as buttons or select menus. The bot routes component interactions by prefix.
type ComponentCommand interface {
	ComponentPrefix() string
	HandleComponent(e *events.ComponentInteractionCreate)
}

// AutocompleteCommand is implemented by commands with autocomplete options.
type AutocompleteCommand interface {
	HandleAutocomplete(e *events.AutocompleteInteractionCreate)
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
//
// A command that also implements bot.EventListener receives every gateway event.
func All(cfg config.Config) []Command {
	cmds := []Command{
		Ping{},
		Weather{},
		NewDeadlockStatistics(deadlockapi.NewService(deadlockapi.NewClient(nil))),
		NewServerStats(nil, nil),
	}

	if cfg.NavidromeUser != "" {
		cmds = append(cmds, NewMusic(navidrome.New(cfg.NavidromeURL, cfg.NavidromeUser, cfg.NavidromePassword)))
	}

	return cmds
}

// interactionUserID returns who triggered an interaction, as a string for
// custom IDs and comparisons.
func interactionUserID(e interface{ User() discord.User }) string {
	return e.User().ID.String()
}
