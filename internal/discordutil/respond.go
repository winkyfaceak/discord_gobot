package discordutil

import (
	"log"

	"github.com/bwmarrin/discordgo"
)

// Respond sends a response to a slash command interaction
//
// Discord slash commands must receive an interaction response
// You cannot use ChannelMessageSend as the first response to a slash command
//
// private controls whether the message is ephemeral:
//
//	private == false
//	    Everyone in the channel can see the response
//
//	private == true
//	    Only the user who ran the command can see the response
func Respond(s *discordgo.Session, i *discordgo.InteractionCreate, message string, private bool) {
	// Default flags are zero, meaning a normal public response
	flags := discordgo.MessageFlags(0)

	// Ephemeral messages are private to the command user
	if private {
		flags = discordgo.MessageFlagsEphemeral
	}

	// InteractionRespond sends the official response to the slash command
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		// ChannelMessageWithSource means:
		//
		// "Respond to the interaction with a message."
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
