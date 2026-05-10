package commands

import (
	"context"
	deadlockapi "discord_gobot/internal/deadlock"
	"discord_gobot/internal/discordutil"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// DeadlockStatistics implements the /deadlock-statistics command.
//
// The Go type is called DeadlockStatistics because that is what this feature is.
// The constructor is called NewDeadlockStatistics because it creates a configured
// command with its required Deadlock API service.
type DeadlockStatistics struct {
	service *deadlockapi.Service
}

// NewDeadlockStatistics creates the /deadlock-statistics command.
func NewDeadlockStatistics(service *deadlockapi.Service) *DeadlockStatistics {
	if service == nil {
		service = deadlockapi.NewService(deadlockapi.NewClient(nil))
	}

	return &DeadlockStatistics{
		service: service,
	}
}

// Definition tells Discord how the slash command should appear.
func (d *DeadlockStatistics) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "deadlock-statistics",
		Description: "Look up Deadlock statistics for a Steam account name.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "account",
				Description: "Steam account/persona name to search for.",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "image",
				Description: "Render a PNG stat card with ImageMagick. Defaults to true.",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "private",
				Description: "Only show the result to you.",
				Required:    false,
			},
		},
	}
}

// Handle runs when someone executes /deadlock-statistics.
func (d *DeadlockStatistics) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := discordutil.OptionMap(i.ApplicationCommandData().Options)

	accountOption, ok := options["account"]
	if !ok {
		discordutil.Respond(s, i, "Missing required input: account", true)
		return
	}

	accountName := strings.TrimSpace(accountOption.StringValue())
	if accountName == "" {
		discordutil.Respond(s, i, "Account name cannot be empty.", true)
		return
	}

	useImage := true
	if imageOption, ok := options["image"]; ok {
		useImage = imageOption.BoolValue()
	}

	private := false
	if privateOption, ok := options["private"]; ok {
		private = privateOption.BoolValue()
	}

	// Deadlock API calls and ImageMagick rendering can take a few seconds.
	// Defer first so Discord does not mark the command as failed.
	discordutil.Defer(s, i, private)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	summary, err := d.service.LookupPlayer(ctx, accountName)
	if err != nil {
		discordutil.EditOriginal(
			s,
			i,
			fmt.Sprintf("Could not get Deadlock statistics for `%s`: %v", accountName, err),
		)
		return
	}

	embed := buildDeadlockStatisticsEmbed(*summary)

	if useImage {
		png, renderErr := deadlockapi.RenderCardPNG(ctx, *summary)
		if renderErr == nil {
			filename := "deadlock-statistics.png"

			embed.Image = &discordgo.MessageEmbedImage{
				URL: "attachment://" + filename,
			}

			discordutil.EditOriginalWithImage(
				s,
				i,
				"Deadlock statistics for **"+summary.Name+"**",
				filename,
				png,
				embed,
			)
			return
		}
	}
	// If ImageMagick is disabled, missing, or fails, the command still works.
	discordutil.EditOriginalEmbed(s, i, embed)
}

func buildDeadlockStatisticsEmbed(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "Record",
			Value:  fmt.Sprintf("%dW / %dL\n%.1f%% WR", summary.Wins, summary.Losses, summary.WinRate),
			Inline: true,
		},
		{
			Name:   "Matches",
			Value:  fmt.Sprintf("%d", summary.Matches),
			Inline: true,
		},
		{
			Name:   "Top Hero",
			Value:  fmt.Sprintf("Hero ID `%d`\n%d matches • %.1f%% WR", summary.TopHeroID, summary.TopHeroMatches, summary.TopHeroWinRate),
			Inline: true,
		},
		{
			Name:   "Average KDA",
			Value:  fmt.Sprintf("%.1f / %.1f / %.1f", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists),
			Inline: true,
		},
		{
			Name:   "Average Souls",
			Value:  formatInt(int(summary.AvgNetWorth)),
			Inline: true,
		},
		{
			Name:   "Average LH / Denies",
			Value:  fmt.Sprintf("%.1f / %.1f", summary.AvgLastHits, summary.AvgDenies),
			Inline: true,
		},
	}

	recent := formatRecentDeadlockMatches(summary.RecentMatches)
	if recent != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Recent Matches",
			Value:  recent,
			Inline: false,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Deadlock Statistics: " + summary.Name,
		Description: fmt.Sprintf("Steam account ID: `%d`", summary.AccountID),
		URL:         summary.ProfileURL,
		Color:       0xff9f1c,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Data from Deadlock API",
		},
	}

	if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: summary.Avatar,
		}
	}

	return embed
}

func formatRecentDeadlockMatches(matches []deadlockapi.RecentMatch) string {
	var lines []string

	for _, match := range matches {
		result := "L"
		if match.Won {
			result = "W"
		}

		lines = append(lines, fmt.Sprintf(
			"`%s` Hero `%d` • %d/%d/%d • %s souls • %dm",
			result,
			match.HeroID,
			match.Kills,
			match.Deaths,
			match.Assists,
			formatInt(int(match.NetWorth)),
			match.DurationMins,
		))
	}

	return strings.Join(lines, "\n")
}

func formatInt(value int) string {
	negative := value < 0
	if negative {
		value = -value
	}

	raw := fmt.Sprintf("%d", value)

	var parts []string
	for len(raw) > 3 {
		parts = append([]string{raw[len(raw)-3:]}, parts...)
		raw = raw[:len(raw)-3]
	}
	parts = append([]string{raw}, parts...)

	result := strings.Join(parts, ",")
	if negative {
		return "-" + result
	}

	return result
}
