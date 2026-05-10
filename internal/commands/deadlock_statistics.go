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
		Description: "View a Deadlock profile card, rank badge, recent matches, or live match status.",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "account",
				Description: "Steam account/persona name, for example: Shroud",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "view",
				Description: "What should the bot show? Defaults to Overview.",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "📊 Overview card", Value: deadlockViewSummary},
					{Name: "🏅 Rank badge", Value: deadlockViewRank},
					{Name: "🕘 Recent matches", Value: deadlockViewRecent},
					{Name: "🟢 Live match status", Value: deadlockViewCurrent},
					{Name: "✨ Everything", Value: deadlockViewAll},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "recent-count",
				Description: "Recent matches to show, 1-10. Defaults to 5.",
				Required:    false,
				MinValue:    floatPtr(1),
				MaxValue:    10,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "image",
				Description: "Attach the generated stat card image. Defaults to true.",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "rank-image",
				Description: "Show rank badge art when rank is visible. Defaults to true.",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "private",
				Description: "Show the result only to you.",
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
		discordutil.Respond(s, i, "I need a Steam account/persona name. Try `/deadlock-statistics account:yourname`.", true)
		return
	}

	accountName := strings.TrimSpace(accountOption.StringValue())
	if accountName == "" {
		discordutil.Respond(s, i, "The account name was empty. Try a Steam persona name like `Shroud`.", true)
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

	discordutil.Defer(s, i, private)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	switch view {
	case deadlockViewRank:
		rankStatus, err := d.service.LookupRank(ctx, accountName)
		if err != nil {
			discordutil.EditOriginal(s, i, friendlyDeadlockError("rank", accountName, err))
			return
		}

		embed := buildDeadlockRankEmbed(*rankStatus, useRankImage)
		discordutil.EditOriginalEmbed(s, i, embed)
		return

	case deadlockViewCurrent:
		currentStatus, err := d.service.LookupCurrentGame(ctx, accountName)
		if err != nil {
			discordutil.EditOriginal(s, i, friendlyDeadlockError("current game", accountName, err))
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
		discordutil.EditOriginal(s, i, friendlyDeadlockError("statistics", accountName, err))
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
				fmt.Sprintf("%s **%s**", viewEmoji(view), summary.Name),
				filename,
				png,
				embed,
			)
			return
		}
	}

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
				Name:   fmt.Sprintf("🕘 Recent Games (%d)", len(summary.RecentMatches)),
				Value:  limitDiscordField(recent),
				Inline: false,
			})
		}
	}

	title := fmt.Sprintf("%s Deadlock Overview — %s", viewEmoji(view), summary.Name)
	if view == deadlockViewRecent {
		title = fmt.Sprintf("🕘 Recent Deadlock Games — %s", summary.Name)
	}

	description := fmt.Sprintf(
		"`%d` • **%d matches** • **%s** win rate\n%s",
		summary.AccountID,
		summary.Matches,
		formatPercent(summary.WinRate),
		winRateBar(summary.WinRate),
	)

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		URL:         summary.ProfileURL,
		Color:       summaryColor(summary.WinRate),
		Fields:      fields,
		Timestamp:   time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Deadlock API • Use view: Rank, Recent, Current, or All",
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
			Name:   "🏆 Record",
			Value:  fmt.Sprintf("**%dW - %dL**\n%s WR", summary.Wins, summary.Losses, formatPercent(summary.WinRate)),
			Inline: true,
		},
		{
			Name:   "🎮 Matches",
			Value:  fmt.Sprintf("**%d** played", summary.Matches),
			Inline: true,
		},
		{
			Name:   "⭐ Most Played Hero",
			Value:  fmt.Sprintf("Hero `%d`\n%d games • %s WR", summary.TopHeroID, summary.TopHeroMatches, formatPercent(summary.TopHeroWinRate)),
			Inline: true,
		},
		{
			Name:   "⚔️ Avg K / D / A",
			Value:  fmt.Sprintf("**%.1f / %.1f / %.1f**", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists),
			Inline: true,
		},
		{
			Name:   "💰 Avg Souls",
			Value:  fmt.Sprintf("**%s**", formatInt(int(summary.AvgNetWorth))),
			Inline: true,
		},
		{
			Name:   "📈 Avg LH / Denies",
			Value:  fmt.Sprintf("**%.1f / %.1f**", summary.AvgLastHits, summary.AvgDenies),
			Inline: true,
		},
	}
}

func buildDeadlockRankEmbed(status deadlockapi.RankStatus, useRankImage bool) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}

	color := 0xff9f1c
	if status.Rank != nil {
		color = rankColor(status.Rank.Badge)
		fields = append(fields,
			&discordgo.MessageEmbedField{
				Name:   "🏅 Predicted Rank",
				Value:  fmt.Sprintf("**%s**", status.Rank.DisplayName()),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "🔢 Badge",
				Value:  fmt.Sprintf("`%d`", status.Rank.Badge),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "📚 Matches Used",
				Value:  fmt.Sprintf("`%d`", status.Rank.MatchesUsed),
				Inline: true,
			},
			&discordgo.MessageEmbedField{
				Name:   "📊 Raw Score",
				Value:  fmt.Sprintf("`%.1f`", status.Rank.RawScore),
				Inline: true,
			},
		)
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "🏅 Predicted Rank",
			Value:  "Rank is currently unavailable for this player.",
			Inline: false,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🏅 Deadlock Rank — " + status.Name,
		Description: fmt.Sprintf("Steam account ID: `%d`", status.AccountID),
		URL:         status.ProfileURL,
		Color:       color,
		Fields:      fields,
		Timestamp:   time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Rank is a Deadlock API prediction, not hidden MMR.",
		},
	}

	if useRankImage && status.Rank != nil && status.Rank.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Rank.ImageURL}
		embed.Image = &discordgo.MessageEmbedImage{URL: status.Rank.ImageURL}
	} else if status.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Avatar}
	}

	return embed
}

func buildDeadlockCurrentGameEmbed(status deadlockapi.CurrentGameStatus) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}
	color := 0x95a5a6
	title := "⚫ Deadlock Status — " + status.Name
	description := fmt.Sprintf("Steam account ID: `%d`", status.AccountID)

	if !status.InGame || status.Match == nil || status.Player == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "⚫ Status",
			Value:  "No active match found right now.",
			Inline: false,
		}, &discordgo.MessageEmbedField{
			Name:   "Note",
			Value:  "Deadlock active-match data can be delayed or incomplete, so this does not always guarantee the player is offline.",
			Inline: false,
		})
	} else {
		match := status.Match
		player := status.Player
		color = 0x2ecc71
		title = "🟢 Live Deadlock Match — " + status.Name
		description = fmt.Sprintf("Currently in game • Steam account ID: `%d`", status.AccountID)

		fields = append(fields,
			&discordgo.MessageEmbedField{Name: "🟢 Status", Value: "**Currently in game**", Inline: true},
			&discordgo.MessageEmbedField{Name: "🆔 Match", Value: formatOptionalInt64(match.MatchID), Inline: true},
			&discordgo.MessageEmbedField{Name: "🦸 Hero", Value: formatOptionalHero(player.HeroID), Inline: true},
			&discordgo.MessageEmbedField{Name: "👥 Team", Value: formatTeam(player), Inline: true},
			&discordgo.MessageEmbedField{Name: "⏱️ Duration", Value: formatDuration(match.DurationS), Inline: true},
			&discordgo.MessageEmbedField{Name: "💰 Team Souls", Value: formatTeamSouls(match, player), Inline: true},
			&discordgo.MessageEmbedField{Name: "🎮 Mode", Value: formatParsedOrUnknown(match.MatchModeParsed), Inline: true},
			&discordgo.MessageEmbedField{Name: "🌍 Region", Value: formatParsedOrUnknown(match.RegionModeParsed), Inline: true},
			&discordgo.MessageEmbedField{Name: "👀 Spectators", Value: formatOptionalInt32(match.Spectators), Inline: true},
		)
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: description,
		URL:         status.ProfileURL,
		Color:       color,
		Fields:      fields,
		Timestamp:   time.Now().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Active match data comes from Deadlock API watch data.",
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
			"🏅 **%s**\nBadge `%d` • Score `%.1f` • `%d` matches used",
			summary.Rank.DisplayName(),
			summary.Rank.Badge,
			summary.Rank.RawScore,
			summary.Rank.MatchesUsed,
		)
	} else if summary.RankError != "" {
		value = "⚠️ Rank unavailable: " + summary.RankError
	}

	return &discordgo.MessageEmbedField{
		Name:   "🏅 Rank",
		Value:  value,
		Inline: false,
	}
}

func currentGameFieldFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbedField {
	value := "Current game was not requested."
	if summary.CurrentGame != nil {
		if summary.CurrentGame.InGame {
			value = "🟢 **Currently in game**"
			if summary.CurrentGame.Match != nil && summary.CurrentGame.Match.MatchID != nil {
				value += fmt.Sprintf(" • Match `%d`", *summary.CurrentGame.Match.MatchID)
			}
			if summary.CurrentGame.Match != nil {
				value += " • " + formatDuration(summary.CurrentGame.Match.DurationS)
			}
		} else {
			value = "⚫ No active match found right now."
		}
	} else if summary.CurrentGameError != "" {
		value = "⚠️ Current game unavailable: " + summary.CurrentGameError
	}

	return &discordgo.MessageEmbedField{
		Name:   "🟢 Current Game",
		Value:  value,
		Inline: false,
	}
}

func formatRecentDeadlockMatches(matches []deadlockapi.RecentMatch) string {
	var lines []string

	for index, match := range matches {
		result := "❌ Loss"
		if match.Won {
			result = "✅ Win"
		}

		started := ""
		if match.StartedUnix > 0 {
			started = fmt.Sprintf(" • <t:%d:R>", match.StartedUnix)
		}

		lines = append(lines, fmt.Sprintf(
			"`#%d` **%s** • Hero `%d` • `%d/%d/%d` KDA • `%s` souls • `%dm`%s\n↳ Match `%d`",
			index+1,
			result,
			match.HeroID,
			match.Kills,
			match.Deaths,
			match.Assists,
			formatInt(int(match.NetWorth)),
			match.DurationMins,
			started,
			match.MatchID,
		))
	}

	return strings.Join(lines, "\n")
}

func friendlyDeadlockError(section string, accountName string, err error) string {
	return fmt.Sprintf(
		"⚠️ I could not get Deadlock %s for `%s`.\n\n**What happened:** %v\n\nTry checking the Steam name spelling, using a more exact persona name, or running the command again in a moment.",
		section,
		accountName,
		err,
	)
}

func viewEmoji(view string) string {
	switch view {
	case deadlockViewRank:
		return "🏅"
	case deadlockViewRecent:
		return "🕘"
	case deadlockViewCurrent:
		return "🟢"
	case deadlockViewAll:
		return "✨"
	default:
		return "📊"
	}
}

func summaryColor(winRate float64) int {
	switch {
	case winRate >= 55:
		return 0x2ecc71
	case winRate >= 50:
		return 0xf1c40f
	case winRate > 0:
		return 0xe67e22
	default:
		return 0x95a5a6
	}
}

func rankColor(badge int32) int {
	switch {
	case badge >= 80:
		return 0x9b59b6
	case badge >= 60:
		return 0x3498db
	case badge >= 40:
		return 0x2ecc71
	case badge >= 20:
		return 0xf1c40f
	default:
		return 0xe67e22
	}
}

func winRateBar(winRate float64) string {
	filled := int(winRate / 10)
	if filled < 0 {
		filled = 0
	}
	if filled > 10 {
		filled = 10
	}

	return strings.Repeat("▰", filled) + strings.Repeat("▱", 10-filled)
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.1f%%", value)
}

func limitDiscordField(value string) string {
	const maxFieldLength = 1024
	if len(value) <= maxFieldLength {
		return value
	}

	const suffix = "\n…more matches hidden. Lower recent-count if this happens."
	limit := maxFieldLength - len(suffix)
	if limit < 0 {
		return value[:maxFieldLength]
	}

	return strings.TrimSpace(value[:limit]) + suffix
}

func formatOptionalHero(value *int32) string {
	if value == nil {
		return "Unknown"
	}

	return fmt.Sprintf("Hero `%d`", *value)
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
