package discordutil

import (
	"bytes"
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
// Use this before slow work such as curl, HTTP APIs, or database calls
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

// EditOriginal replaces the deferred response with final content
func EditOriginal(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &message,
	})

	if err != nil {
		log.Printf("error editing interaction response: %v", err)
	}
}

// FollowUp sends an extra message after the original interaction response.
//
// This is useful when the content is too long for one Discord message.
func FollowUp(s *discordgo.Session, i *discordgo.InteractionCreate, message string, private bool) {
	flags := discordgo.MessageFlags(0)

	if private {
		flags = discordgo.MessageFlagsEphemeral
	}

	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: message,
		Flags:   flags,
	})

	if err != nil {
		log.Printf("error sending follow-up message: %v", err)
	}
}

// EditOriginalWithImage edits the original interaction response and attaches
// a PNG image.
//
// The embed can reference the image using:
//
//	attachment://filename.png
func EditOriginalWithImage(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	message string,
	filename string,
	imageBytes []byte,
	embed *discordgo.MessageEmbed,
) {
	embeds := []*discordgo.MessageEmbed{embed}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &message,
		Embeds:  &embeds,
		Files: []*discordgo.File{
			{
				Name:        filename,
				ContentType: "image/png",
				Reader:      bytes.NewReader(imageBytes),
			},
		},
	})

	if err != nil {
		log.Printf("error editing interaction response with image: %v", err)
	}
}

// EditOriginalEmbed edits the original deferred interaction response using
// a Discord embed.
func EditOriginalEmbed(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	embed *discordgo.MessageEmbed,
) {
	embeds := []*discordgo.MessageEmbed{embed}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds: &embeds,
	})

	if err != nil {
		log.Printf("error editing interaction response with embed: %v", err)
	}
}

// EditOriginalEmbedWithComponents edits the original deferred interaction response
// with an embed and Discord components such as buttons.
func EditOriginalEmbedWithComponents(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	embed *discordgo.MessageEmbed,
	components []discordgo.MessageComponent,
) {
	embeds := []*discordgo.MessageEmbed{embed}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &components,
	})

	if err != nil {
		log.Printf("error editing interaction response with embed/components: %v", err)
	}
}

// EditOriginalImageWithComponents creates an image-backed interactive response
// and returns its message identity for later public channel edits.
func EditOriginalImageWithComponents(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	filename string,
	imageBytes []byte,
	embed *discordgo.MessageEmbed,
	components []discordgo.MessageComponent,
) (*discordgo.Message, error) {
	embeds := []*discordgo.MessageEmbed{embed}
	attachments := []*discordgo.MessageAttachment{}

	return s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:      &embeds,
		Components:  &components,
		Attachments: &attachments,
		Files: []*discordgo.File{
			{
				Name:        filename,
				ContentType: "image/png",
				Reader:      bytes.NewReader(imageBytes),
			},
		},
	})
}

// EditChannelImageWithComponents replaces a public interactive card's image
// and controls after its originating interaction has completed.
func EditChannelImageWithComponents(
	s *discordgo.Session,
	channelID string,
	messageID string,
	filename string,
	imageBytes []byte,
	embed *discordgo.MessageEmbed,
	components []discordgo.MessageComponent,
) (*discordgo.Message, error) {
	embeds := []*discordgo.MessageEmbed{embed}
	attachments := []*discordgo.MessageAttachment{}

	return s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:          messageID,
		Channel:     channelID,
		Embeds:      &embeds,
		Components:  &components,
		Attachments: &attachments,
		Files: []*discordgo.File{
			{
				Name:        filename,
				ContentType: "image/png",
				Reader:      bytes.NewReader(imageBytes),
			},
		},
	})
}

// DeferComponentUpdate acknowledges a button/select-menu click and lets the bot
// edit the message after slower API work completes.
func DeferComponentUpdate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredMessageUpdate,
	})

	if err != nil {
		log.Printf("error deferring component update: %v", err)
	}
}

// UpdateComponentMessage updates the original message attached to a component.
func UpdateComponentMessage(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	embed *discordgo.MessageEmbed,
	components []discordgo.MessageComponent,
) {
	embeds := []*discordgo.MessageEmbed{embed}
	attachments := []*discordgo.MessageAttachment{}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:      &embeds,
		Components:  &components,
		Attachments: &attachments,
	})

	if err != nil {
		log.Printf("error updating component message: %v", err)
	}
}

// UpdateComponentImageWithComponents replaces an interactive response image
// and controls after a component update has been deferred.
func UpdateComponentImageWithComponents(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	filename string,
	imageBytes []byte,
	embed *discordgo.MessageEmbed,
	components []discordgo.MessageComponent,
) error {
	embeds := []*discordgo.MessageEmbed{embed}
	attachments := []*discordgo.MessageAttachment{}

	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:      &embeds,
		Components:  &components,
		Attachments: &attachments,
		Files: []*discordgo.File{
			{
				Name:        filename,
				ContentType: "image/png",
				Reader:      bytes.NewReader(imageBytes),
			},
		},
	})

	return err
}

// RespondEphemeralToComponent sends a private message for a component click.
func RespondEphemeralToComponent(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})

	if err != nil {
		log.Printf("error responding ephemerally to component: %v", err)
	}
}
