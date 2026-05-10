package discordutil

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

// Respond sends an immediate response to a slash command interaction.
//
// Use this for fast commands like /ping.
func Respond(s *discordgo.Session, i *discordgo.InteractionCreate, message string, private bool) {
	flags := discordgo.MessageFlags(0)

	if private {
		flags = discordgo.MessageFlagsEphemeral
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   flags,
		},
	})

	if err != nil {
		log.Printf("error responding to interaction: %v", err)
	}
}

// Defer tells Discord:
//
//	"I received the command, but I need more time."
//
// Use this for commands that call APIs, databases, curl, or anything slow
func Defer(s *discordgo.Session, i *discordgo.InteractionCreate, private bool) {
	flags := discordgo.MessageFlags(0)

	if private {
		flags = discordgo.MessageFlagsEphemeral
	}

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: flags,
		},
	})

	if err != nil {
		log.Printf("error deferring interaction: %v", err)
	}
}

// EditOriginal replaces the deferred "thinking..." response with the final text.
func EditOriginal(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &message,
	})

	if err != nil {
		log.Printf("error editing interaction response: %v", err)
	}
}
