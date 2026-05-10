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

const (
	deadlockViewSummary = "summary"
	deadlockViewRank    = "rank"
	deadlockViewRecent  = "recent"
	deadlockViewCurrent = "current"
	deadlockViewAll     = "all"
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
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "view",
				Description: "Choose which Deadlock section to show. Defaults to summary.",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Summary", Value: deadlockViewSummary},
					{Name: "Rank image", Value: deadlockViewRank},
					{Name: "Recent games", Value: deadlockViewRecent},
					{Name: "Current game/status", Value: deadlockViewCurrent},
					{Name: "All", Value: deadlockViewAll},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "recent-count",
				Description: "How many recent games to show, from 1 to 10. Defaults to 5.",
				Required:    false,
				MinValue:    floatPtr(1),
				MaxValue:    10,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "image",
				Description: "Render a PNG stat card with ImageMagick. Defaults to true.",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "rank-image",
				Description: "Show the rank badge image when the view includes rank. Defaults to true.",
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

	view := deadlockViewSummary
	if viewOption, ok := options["view"]; ok {
		view = strings.TrimSpace(viewOption.StringValue())
	}

	recentLimit := 5
	if recentOption, ok := options["recent-count"]; ok {
		recentLimit = int(recentOption.IntValue())
	}
	if recentLimit < 1 {
		recentLimit = 1
	}
	if recentLimit > 10 {
		recentLimit = 10
	}

	useImage := true
	if imageOption, ok := options["image"]; ok {
		useImage = imageOption.BoolValue()
	}

	useRankImage := true
	if rankImageOption, ok := options["rank-image"]; ok {
		useRankImage = rankImageOption.BoolValue()
	}

	private := false
	if privateOption, ok := options["private"]; ok {
		private = privateOption.BoolValue()
	}

	// Deadlock API calls and ImageMagick rendering can take a few seconds.
	// Defer first so Discord does not mark the command as failed.
	discordutil.Defer(s, i, private)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	switch view {
	case deadlockViewRank:
		rankStatus, err := d.service.LookupRank(ctx, accountName)
		if err != nil {
			discordutil.EditOriginal(
				s,
				i,
				fmt.Sprintf("Could not get Deadlock rank for `%s`: %v", accountName, err),
			)
			return
		}

		embed := buildDeadlockRankEmbed(*rankStatus, useRankImage)
		discordutil.EditOriginalEmbed(s, i, embed)
		return

	case deadlockViewCurrent:
		currentStatus, err := d.service.LookupCurrentGame(ctx, accountName)
		if err != nil {
			discordutil.EditOriginal(
				s,
				i,
				fmt.Sprintf("Could not get current Deadlock game for `%s`: %v", accountName, err),
			)
			return
		}

		embed := buildDeadlockCurrentGameEmbed(*currentStatus)
		discordutil.EditOriginalEmbed(s, i, embed)
		return
	}

	summary, err := d.service.LookupPlayerWithOptions(ctx, accountName, deadlockapi.PlayerLookupOptions{
		RecentLimit:    recentLimit,
		IncludeRank:    view == deadlockViewAll,
		IncludeCurrent: view == deadlockViewAll,
	})
	if err != nil {
		discordutil.EditOriginal(
			s,
			i,
			fmt.Sprintf("Could not get Deadlock statistics for `%s`: %v", accountName, err),
		)
		return
	}

	embed := buildDeadlockStatisticsEmbed(*summary, view, useRankImage)

	canRenderStatCard := useImage && (view == deadlockViewSummary || view == deadlockViewAll)
	if canRenderStatCard {
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

func buildDeadlockStatisticsEmbed(summary deadlockapi.PlayerSummary, view string, useRankImage bool) *discordgo.MessageEmbed {
	fields := make([]*discordgo.MessageEmbedField, 0, 12)

	if view == deadlockViewSummary || view == deadlockViewAll || view == "" {
		fields = append(fields, summaryFields(summary)...)
	}

	if view == deadlockViewAll {
		fields = append(fields, rankFieldFromSummary(summary), currentGameFieldFromSummary(summary))
	}

	if view == deadlockViewRecent || view == deadlockViewSummary || view == deadlockViewAll || view == "" {
		recent := formatRecentDeadlockMatches(summary.RecentMatches)
		if recent != "" {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   "Recent Games",
				Value:  recent,
				Inline: false,
			})
		}
	}

	title := "Deadlock Statistics: " + summary.Name
	if view == deadlockViewRecent {
		title = "Recent Deadlock Games: " + summary.Name
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: fmt.Sprintf("Steam account ID: `%d`", summary.AccountID),
		URL:         summary.ProfileURL,
		Color:       0xff9f1c,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Data from Deadlock API",
		},
	}

	if view == deadlockViewAll && useRankImage && summary.Rank != nil && summary.Rank.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Rank.ImageURL}
	} else if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Avatar}
	}

	return embed
}

func summaryFields(summary deadlockapi.PlayerSummary) []*discordgo.MessageEmbedField {
	return []*discordgo.MessageEmbedField{
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
}

func buildDeadlockRankEmbed(status deadlockapi.RankStatus, useRankImage bool) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}

	if status.Rank != nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Predicted Rank",
			Value: fmt.Sprintf(
				"**%s**\nBadge `%d` • Raw score `%.1f` • `%d` matches used",
				status.Rank.DisplayName(),
				status.Rank.Badge,
				status.Rank.RawScore,
				status.Rank.MatchesUsed,
			),
			Inline: false,
		})
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Predicted Rank",
			Value:  "Rank unavailable.",
			Inline: false,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Deadlock Rank: " + status.Name,
		Description: fmt.Sprintf("Steam account ID: `%d`", status.AccountID),
		URL:         status.ProfileURL,
		Color:       0xff9f1c,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Rank is a Deadlock API ML prediction, not hidden MMR.",
		},
	}

	if useRankImage && status.Rank != nil && status.Rank.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Rank.ImageURL}
	} else if status.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Avatar}
	}

	return embed
}

func buildDeadlockCurrentGameEmbed(status deadlockapi.CurrentGameStatus) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}

	if !status.InGame || status.Match == nil || status.Player == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Status",
			Value:  "No active match found for this player in the Deadlock API watch data.",
			Inline: false,
		})
	} else {
		match := status.Match
		player := status.Player

		fields = append(fields,
			&discordgo.MessageEmbedField{Name: "Status", Value: "Currently in game", Inline: true},
			&discordgo.MessageEmbedField{Name: "Match ID", Value: formatOptionalInt64(match.MatchID), Inline: true},
			&discordgo.MessageEmbedField{Name: "Hero", Value: formatOptionalInt32(player.HeroID), Inline: true},
			&discordgo.MessageEmbedField{Name: "Team", Value: formatTeam(player), Inline: true},
			&discordgo.MessageEmbedField{Name: "Duration", Value: formatDuration(match.DurationS), Inline: true},
			&discordgo.MessageEmbedField{Name: "Team Souls", Value: formatTeamSouls(match, player), Inline: true},
			&discordgo.MessageEmbedField{Name: "Mode", Value: formatParsedOrUnknown(match.MatchModeParsed), Inline: true},
			&discordgo.MessageEmbedField{Name: "Region", Value: formatParsedOrUnknown(match.RegionModeParsed), Inline: true},
			&discordgo.MessageEmbedField{Name: "Spectators", Value: formatOptionalInt32(match.Spectators), Inline: true},
		)
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Deadlock Current Game: " + status.Name,
		Description: fmt.Sprintf("Steam account ID: `%d`", status.AccountID),
		URL:         status.ProfileURL,
		Color:       0xff9f1c,
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Active match data can be limited by Deadlock API watch data.",
		},
	}

	if status.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Avatar}
	}

	return embed
}

func rankFieldFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbedField {
	value := "Rank was not requested."
	if summary.Rank != nil {
		value = fmt.Sprintf(
			"**%s**\nBadge `%d` • Raw score `%.1f` • `%d` matches used",
			summary.Rank.DisplayName(),
			summary.Rank.Badge,
			summary.Rank.RawScore,
			summary.Rank.MatchesUsed,
		)
	} else if summary.RankError != "" {
		value = "Unavailable: " + summary.RankError
	}

	return &discordgo.MessageEmbedField{
		Name:   "Rank",
		Value:  value,
		Inline: false,
	}
}

func currentGameFieldFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbedField {
	value := "Current game was not requested."
	if summary.CurrentGame != nil {
		if summary.CurrentGame.InGame {
			value = "Currently in game"
			if summary.CurrentGame.Match != nil && summary.CurrentGame.Match.MatchID != nil {
				value += fmt.Sprintf(" • Match `%d`", *summary.CurrentGame.Match.MatchID)
			}
			if summary.CurrentGame.Match != nil {
				value += " • " + formatDuration(summary.CurrentGame.Match.DurationS)
			}
		} else {
			value = "No active match found for this player."
		}
	} else if summary.CurrentGameError != "" {
		value = "Unavailable: " + summary.CurrentGameError
	}

	return &discordgo.MessageEmbedField{
		Name:   "Current Game",
		Value:  value,
		Inline: false,
	}
}

func formatRecentDeadlockMatches(matches []deadlockapi.RecentMatch) string {
	var lines []string

	for _, match := range matches {
		result := "L"
		if match.Won {
			result = "W"
		}

		started := ""
		if match.StartedUnix > 0 {
			started = fmt.Sprintf(" • <t:%d:R>", match.StartedUnix)
		}

		lines = append(lines, fmt.Sprintf(
			"`%s` Match `%d` • Hero `%d` • %d/%d/%d • %s souls • %dm%s",
			result,
			match.MatchID,
			match.HeroID,
			match.Kills,
			match.Deaths,
			match.Assists,
			formatInt(int(match.NetWorth)),
			match.DurationMins,
			started,
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

func formatOptionalInt32(value *int32) string {
	if value == nil {
		return "Unknown"
	}

	return fmt.Sprintf("%d", *value)
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return "Unknown"
	}

	return fmt.Sprintf("%d", *value)
}

func formatParsedOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}

	return value
}

func formatTeam(player *deadlockapi.ActiveMatchPlayer) string {
	if player == nil {
		return "Unknown"
	}

	if strings.TrimSpace(player.TeamParsed) != "" {
		return player.TeamParsed
	}

	return formatOptionalInt32(player.Team)
}

func formatTeamSouls(match *deadlockapi.ActiveMatch, player *deadlockapi.ActiveMatchPlayer) string {
	if match == nil || player == nil || player.Team == nil {
		return "Unknown"
	}

	netWorth := match.NetWorthForTeam(*player.Team)
	if netWorth == nil {
		return "Unknown"
	}

	return formatInt(int(*netWorth))
}

func formatDuration(seconds *int32) string {
	if seconds == nil || *seconds < 0 {
		return "Unknown"
	}

	minutes := *seconds / 60
	remainingSeconds := *seconds % 60
	return fmt.Sprintf("%dm %02ds", minutes, remainingSeconds)
}

func floatPtr(value float64) *float64 {
	return &value
}
