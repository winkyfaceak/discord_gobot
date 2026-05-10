package discordutil

import "github.com/bwmarrin/discordgo"

// OptionMap converts DiscordGo's slash command options from a slice into a map
//
// DiscordGo gives you options like this:
//
//	[]*discordgo.ApplicationCommandInteractionDataOption
//
// # That means looking up an option by name requires a loop
//
// This helper lets command handlers do:
//
//	options := OptionMap(i.ApplicationCommandData().Options)
//	name := options["name"].StringValue()
func OptionMap(options []*discordgo.ApplicationCommandInteractionDataOption) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	result := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)

	for _, option := range options {
		result[option.Name] = option
	}

	return result
}
