package commands

import (
	"regexp"
	"slices"
	"strings"

	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
)

// FixLinkCommands are /fixlink and the "Fix X link" message command: they
// privately reply with fixupx.com versions of X/Twitter post links, which
// Discord embeds with playable media (so GIFs can be starred).
func FixLinkCommands() []Command {
	return []Command{
		funcCommand{
			def: discord.SlashCommandCreate{
				Name:        "fixlink",
				Description: "Get an X/Twitter link that embeds properly in Discord (only you see it)",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{Name: "link", Description: "The x.com or twitter.com post link", Required: true},
				},
			},
			handle: func(e *events.ApplicationCommandInteractionCreate) {
				replyFixedLinks(e, e.SlashCommandInteractionData().String("link"), "That isn't an X/Twitter post link.")
			},
		},
		funcCommand{
			def: discord.MessageCommandCreate{Name: "Fix X link"},
			handle: func(e *events.ApplicationCommandInteractionCreate) {
				msg := e.MessageCommandInteractionData().TargetMessage()
				replyFixedLinks(e, msg.Content, "There are no X/Twitter post links in that message.")
			},
		},
	}
}

func replyFixedLinks(e *events.ApplicationCommandInteractionCreate, text, none string) {
	links := fixXLinks(text)
	if len(links) == 0 {
		discordutil.Reply(e, none, true)
		return
	}
	discordutil.Reply(e, strings.Join(links, "\n"), true)
}

// xPost matches x.com and twitter.com post links (with or without www. or
// mobile.), capturing the user/status/ID part.
var xPost = regexp.MustCompile(`https?://(?:www\.|mobile\.)?(?:x|twitter)\.com/(\w+/status/\d+)`)

// fixXLinks returns each X post link in text as a fixupx.com link, without
// tracking parameters like ?s=20, each once.
func fixXLinks(text string) []string {
	var fixed []string
	for _, match := range xPost.FindAllStringSubmatch(text, -1) {
		link := "https://fixupx.com/" + match[1]
		if !slices.Contains(fixed, link) {
			fixed = append(fixed, link)
		}
	}
	return fixed
}
