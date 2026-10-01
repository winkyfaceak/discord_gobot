package discordutil

import (
	"bytes"
	"log"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
)

// Replier is any interaction event that can answer with a new message
// (slash commands and button clicks).
type Replier interface {
	CreateMessage(discord.MessageCreate, ...rest.RequestOpt) error
}

// Interaction is any interaction event whose original response can be edited.
type Interaction interface {
	Client() *bot.Client
	ApplicationID() snowflake.ID
	Token() string
}

// Reply sends text as the immediate interaction response.
func Reply(e Replier, text string, private bool) {
	msg := discord.MessageCreate{Content: text}
	if private {
		msg.Flags = discord.MessageFlagEphemeral
	}
	if err := e.CreateMessage(msg); err != nil {
		log.Printf("error responding to interaction: %v", err)
	}
}

// EditOriginal replaces the (usually deferred) interaction response.
func EditOriginal(e Interaction, update discord.MessageUpdate) (*discord.Message, error) {
	return e.Client().Rest.UpdateInteractionResponse(e.ApplicationID(), e.Token(), update)
}

// EditText replaces the deferred response with text.
func EditText(e Interaction, text string) {
	if _, err := EditOriginal(e, discord.MessageUpdate{Content: &text}); err != nil {
		log.Printf("error editing interaction response: %v", err)
	}
}

// FollowUp sends an extra message after the original interaction response.
//
// This is useful when the content is too long for one Discord message.
func FollowUp(e Interaction, text string, private bool) {
	msg := discord.MessageCreate{Content: text}
	if private {
		msg.Flags = discord.MessageFlagEphemeral
	}
	if _, err := e.Client().Rest.CreateFollowupMessage(e.ApplicationID(), e.Token(), msg); err != nil {
		log.Printf("error sending follow-up message: %v", err)
	}
}

// ImageUpdate swaps a message's content for a PNG card shown in the embed,
// removing any earlier attachment. alt is the image's alt text; Discord only
// keeps a new file on an edit when it has one.
func ImageUpdate(filename string, alt string, png []byte, embed discord.Embed, components []discord.LayoutComponent) discord.MessageUpdate {
	if components == nil {
		components = []discord.LayoutComponent{} // an empty list removes old buttons
	}
	embed.Image = &discord.EmbedResource{URL: "attachment://" + filename}
	return discord.MessageUpdate{
		Embeds:      &[]discord.Embed{embed},
		Components:  &components,
		Attachments: &[]discord.AttachmentUpdate{},
		Files:       []*discord.File{discord.NewFile(filename, alt, bytes.NewReader(png))},
	}
}

// EmbedUpdate swaps a message's content for an embed, removing any attachment.
func EmbedUpdate(embed discord.Embed, components []discord.LayoutComponent) discord.MessageUpdate {
	if components == nil {
		components = []discord.LayoutComponent{} // an empty list removes old buttons
	}
	return discord.MessageUpdate{
		Embeds:      &[]discord.Embed{embed},
		Components:  &components,
		Attachments: &[]discord.AttachmentUpdate{},
	}
}
