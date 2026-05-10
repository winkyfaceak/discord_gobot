package commands

import (
	"context"
	deadlockapi "discord_gobot/internal/deadlock"
	"discord_gobot/internal/discordutil"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	deadlockComponentPrefix = "deadlock"

	deadlockViewOverview = "overview"
	deadlockViewRank     = "rank"
	deadlockViewRecent   = "recent"
	deadlockViewCurrent  = "current"
	deadlockViewBuilds   = "builds"
	deadlockViewAll      = "all"

	defaultDeadlockRecentLimit = 10
	maxDeadlockRecentLimit     = 20
	deadlockRecentPageSize     = 5

	deadlockColorGold    = 0xff9f1c
	deadlockColorGreen   = 0x57f287
	deadlockColorRed     = 0xed4245
	deadlockColorNeutral = 0x5865f2
)

// DeadlockStatistics implements the /deadlock-statistics command.
type DeadlockStatistics struct {
	service *deadlockapi.Service

	mu       sync.Mutex
	sessions map[string]deadlockSession
}

type deadlockSession struct {
	Profile      deadlockapi.SteamProfile
	RecentLimit  int
	UseRankImage bool
	UpdatedAt    time.Time
}

type deadlockComponentState struct {
	OwnerID      string
	AccountID    int64
	View         string
	Page         int
	RecentLimit  int
	UseRankImage bool
}

// NewDeadlockStatistics creates the /deadlock-statistics command.
func NewDeadlockStatistics(service *deadlockapi.Service) *DeadlockStatistics {
	if service == nil {
		service = deadlockapi.NewService(deadlockapi.NewClient(nil))
	}

	return &DeadlockStatistics{
		service:  service,
		sessions: make(map[string]deadlockSession),
	}
}

func (d *DeadlockStatistics) ComponentPrefix() string {
	return deadlockComponentPrefix
}

// Definition tells Discord how the slash command should appear.
func (d *DeadlockStatistics) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "deadlock-statistics",
		Description: "Interactive Deadlock player statistics, rank, recent games, current game, and builds.",
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
				Description: "Initial page to open. Buttons let you switch pages after that.",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Overview", Value: deadlockViewOverview},
					{Name: "Rank image", Value: deadlockViewRank},
					{Name: "Recent games", Value: deadlockViewRecent},
					{Name: "Current game/status", Value: deadlockViewCurrent},
					{Name: "Builds & items", Value: deadlockViewBuilds},
					{Name: "All snapshot", Value: deadlockViewAll},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "recent-count",
				Description: "How many recent games to keep available, from 1 to 20. Defaults to 10.",
				Required:    false,
				MinValue:    floatPtr(1),
				MaxValue:    maxDeadlockRecentLimit,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "rank-image",
				Description: "Show the rank badge image when rank is displayed. Defaults to true.",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "interactive",
				Description: "Show Discord buttons for page navigation. Defaults to true.",
				Required:    false,
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "image",
				Description: "Render the old PNG stat card instead of buttons. Only used when interactive is false.",
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

	view := deadlockViewOverview
	if viewOption, ok := options["view"]; ok {
		view = cleanDeadlockView(viewOption.StringValue())
	}

	recentLimit := defaultDeadlockRecentLimit
	if recentOption, ok := options["recent-count"]; ok {
		recentLimit = int(recentOption.IntValue())
	}
	recentLimit = clampInt(recentLimit, 1, maxDeadlockRecentLimit)

	useRankImage := true
	if rankImageOption, ok := options["rank-image"]; ok {
		useRankImage = rankImageOption.BoolValue()
	}

	interactive := true
	if interactiveOption, ok := options["interactive"]; ok {
		interactive = interactiveOption.BoolValue()
	}

	useImage := false
	if imageOption, ok := options["image"]; ok {
		useImage = imageOption.BoolValue()
	}

	private := false
	if privateOption, ok := options["private"]; ok {
		private = privateOption.BoolValue()
	}

	discordutil.Defer(s, i, private)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := d.service.LookupPlayerWithOptions(ctx, accountName, deadlockLookupOptionsForView(view, recentLimit))
	if err != nil {
		discordutil.EditOriginal(
			s,
			i,
			fmt.Sprintf("Could not get Deadlock statistics for `%s`: %v", accountName, err),
		)
		return
	}

	ownerID := interactionUserID(i)
	profile := deadlockapi.SteamProfile{
		AccountID:   summary.AccountID,
		PersonaName: summary.Name,
		ProfileURL:  summary.ProfileURL,
		Avatar:      summary.Avatar,
	}
	d.storeSession(ownerID, profile, recentLimit, useRankImage)

	state := deadlockComponentState{
		OwnerID:      ownerID,
		AccountID:    summary.AccountID,
		View:         view,
		Page:         0,
		RecentLimit:  recentLimit,
		UseRankImage: useRankImage,
	}

	embed := buildDeadlockEmbed(*summary, state)

	if interactive {
		discordutil.EditOriginalEmbedWithComponents(s, i, embed, buildDeadlockComponents(state, len(summary.RecentMatches)))
		return
	}

	if useImage && (view == deadlockViewOverview || view == deadlockViewAll) {
		png, renderErr := deadlockapi.RenderCardPNG(ctx, *summary)
		if renderErr == nil {
			filename := "deadlock-statistics.png"
			embed.Image = &discordgo.MessageEmbedImage{URL: "attachment://" + filename}
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

	discordutil.EditOriginalEmbed(s, i, embed)
}

func (d *DeadlockStatistics) HandleComponent(s *discordgo.Session, i *discordgo.InteractionCreate) {
	state, err := parseDeadlockComponentState(i.MessageComponentData().CustomID)
	if err != nil {
		discordutil.RespondEphemeralToComponent(s, i, "That Deadlock menu is too old or invalid. Run `/deadlock-statistics` again.")
		return
	}

	clickerID := interactionUserID(i)
	if state.OwnerID != "" && clickerID != "" && state.OwnerID != clickerID {
		discordutil.RespondEphemeralToComponent(s, i, "This Deadlock menu belongs to the user who opened it. Run your own `/deadlock-statistics` command to browse freely.")
		return
	}

	discordutil.DeferComponentUpdate(s, i)

	profile := d.sessionProfile(state)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	summary, err := d.service.LookupPlayerByProfile(ctx, profile, deadlockLookupOptionsForView(state.View, state.RecentLimit))
	if err != nil {
		discordutil.UpdateComponentMessage(
			s,
			i,
			buildDeadlockErrorEmbed(profile, err),
			buildDeadlockComponents(state, 0),
		)
		return
	}

	if profile.DisplayName() == fmt.Sprintf("Account %d", state.AccountID) {
		profile.PersonaName = summary.Name
		profile.ProfileURL = summary.ProfileURL
		profile.Avatar = summary.Avatar
	}
	d.storeSession(state.OwnerID, profile, state.RecentLimit, state.UseRankImage)

	state.Page = clampRecentPage(state.Page, len(summary.RecentMatches))
	embed := buildDeadlockEmbed(*summary, state)
	discordutil.UpdateComponentMessage(s, i, embed, buildDeadlockComponents(state, len(summary.RecentMatches)))
}

func deadlockLookupOptionsForView(view string, recentLimit int) deadlockapi.PlayerLookupOptions {
	return deadlockapi.PlayerLookupOptions{
		RecentLimit:    recentLimit,
		IncludeRank:    view == deadlockViewRank || view == deadlockViewAll,
		IncludeCurrent: view == deadlockViewCurrent || view == deadlockViewAll,
		IncludeBuilds:  view == deadlockViewBuilds || view == deadlockViewAll,
	}
}

func buildDeadlockEmbed(summary deadlockapi.PlayerSummary, state deadlockComponentState) *discordgo.MessageEmbed {
	switch state.View {
	case deadlockViewRank:
		return buildDeadlockRankEmbedFromSummary(summary, state.UseRankImage)
	case deadlockViewRecent:
		return buildDeadlockRecentEmbed(summary, state)
	case deadlockViewCurrent:
		return buildDeadlockCurrentGameEmbedFromSummary(summary)
	case deadlockViewBuilds:
		return buildDeadlockBuildsEmbed(summary)
	case deadlockViewAll:
		return buildDeadlockAllSnapshotEmbed(summary, state.UseRankImage)
	default:
		return buildDeadlockOverviewEmbed(summary)
	}
}

func buildDeadlockOverviewEmbed(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbed {
	fields := summaryFields(summary)

	recent := formatRecentDeadlockMatches(summary.RecentMatches[:minInt(len(summary.RecentMatches), 3)])
	if recent != "" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "🕘 Latest Games",
			Value:  recent,
			Inline: false,
		})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🟧 Deadlock Player Hub • " + summary.Name,
		Description: deadlockSummaryDescription(summary, "Use the buttons below to jump between rank, recent games, live status, and builds."),
		URL:         summary.ProfileURL,
		Color:       deadlockEmbedColor(summary),
		Fields:      fields,
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Overview • Rank • Recent • Current game • Builds & items",
		},
	}

	if summary.TopHeroIconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.TopHeroIconURL}
	} else if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Avatar}
	}

	return embed
}

func buildDeadlockAllSnapshotEmbed(summary deadlockapi.PlayerSummary, useRankImage bool) *discordgo.MessageEmbed {
	fields := summaryFields(summary)
	fields = append(fields, rankFieldFromSummary(summary), currentGameFieldFromSummary(summary), buildFieldFromSummary(summary))

	recent := formatRecentDeadlockMatches(summary.RecentMatches[:minInt(len(summary.RecentMatches), 5)])
	if recent != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "🕘 Recent Form", Value: recent, Inline: false})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "✨ Deadlock Snapshot • " + summary.Name,
		Description: deadlockSummaryDescription(summary, "Full snapshot can be slower because it checks multiple Deadlock API sections."),
		URL:         summary.ProfileURL,
		Color:       deadlockEmbedColor(summary),
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Snapshot includes overview, rank, current game, builds, and recent form."},
	}

	if useRankImage && summary.Rank != nil && summary.Rank.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Rank.ImageURL}
	} else if summary.TopHeroIconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.TopHeroIconURL}
	} else if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Avatar}
	}

	return embed
}

func summaryFields(summary deadlockapi.PlayerSummary) []*discordgo.MessageEmbedField {
	topHero := fmt.Sprintf("Hero ID `%d`", summary.TopHeroID)
	if summary.TopHeroName != "" {
		topHero = fmt.Sprintf("**%s**\n`Hero %d`", summary.TopHeroName, summary.TopHeroID)
	}
	topHero += fmt.Sprintf("\n%s matches • **%.1f%% WR**", formatInt(summary.TopHeroMatches), summary.TopHeroWinRate)

	return []*discordgo.MessageEmbedField{
		{Name: "🏆 Record", Value: fmt.Sprintf("**%dW / %dL**\n%s", summary.Wins, summary.Losses, formatRecordDifferential(summary.Wins, summary.Losses)), Inline: true},
		{Name: "📈 Win Rate", Value: fmt.Sprintf("**%.1f%%**\n%s", summary.WinRate, formatWinRateLabel(summary.WinRate, summary.Matches)), Inline: true},
		{Name: "🎭 Top Hero", Value: topHero, Inline: true},
		{Name: "⚔️ Avg KDA", Value: fmt.Sprintf("**%.1f / %.1f / %.1f**", summary.AvgKills, summary.AvgDeaths, summary.AvgAssists), Inline: true},
		{Name: "💰 Avg Souls", Value: fmt.Sprintf("**%s**", formatInt(int(summary.AvgNetWorth))), Inline: true},
		{Name: "🎯 LH / Denies", Value: fmt.Sprintf("**%.1f / %.1f**", summary.AvgLastHits, summary.AvgDenies), Inline: true},
	}
}

func buildDeadlockRankEmbedFromSummary(summary deadlockapi.PlayerSummary, useRankImage bool) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{rankFieldFromSummary(summary)}

	embed := &discordgo.MessageEmbed{
		Title:       "🏅 Deadlock Rank • " + summary.Name,
		Description: deadlockSummaryDescription(summary, "Rank is a Deadlock API ML prediction, not hidden MMR."),
		URL:         summary.ProfileURL,
		Color:       deadlockEmbedColor(summary),
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Rank badges are prediction data from the Deadlock API."},
	}

	if useRankImage && summary.Rank != nil && summary.Rank.ImageURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Rank.ImageURL}
	} else if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Avatar}
	}

	return embed
}

func buildDeadlockRecentEmbed(summary deadlockapi.PlayerSummary, state deadlockComponentState) *discordgo.MessageEmbed {
	page := clampRecentPage(state.Page, len(summary.RecentMatches))
	start := page * deadlockRecentPageSize
	end := minInt(start+deadlockRecentPageSize, len(summary.RecentMatches))
	pageMatches := []deadlockapi.RecentMatch{}
	if start < len(summary.RecentMatches) {
		pageMatches = summary.RecentMatches[start:end]
	}

	totalPages := 1
	if len(summary.RecentMatches) > 0 {
		totalPages = (len(summary.RecentMatches) + deadlockRecentPageSize - 1) / deadlockRecentPageSize
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Match Cards", Value: emptyFallback(formatRecentDeadlockMatches(pageMatches), "No recent games returned."), Inline: false},
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🕘 Recent Deadlock Games • " + summary.Name,
		Description: fmt.Sprintf("%s\nPage `%d/%d`", deadlockSummaryDescription(summary, ""), page+1, totalPages),
		URL:         summary.ProfileURL,
		Color:       deadlockEmbedColor(summary),
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Use ◀ and ▶ to browse recent matches."},
	}

	if len(pageMatches) > 0 && pageMatches[0].HeroIconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: pageMatches[0].HeroIconURL}
	} else if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Avatar}
	}

	return embed
}

func buildDeadlockCurrentGameEmbedFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbed {
	if summary.CurrentGameError != "" {
		return &discordgo.MessageEmbed{
			Title:       "🎮 Deadlock Current Game • " + summary.Name,
			Description: deadlockSummaryDescription(summary, ""),
			Color:       deadlockEmbedColor(summary),
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Status", Value: "Unavailable: " + summary.CurrentGameError, Inline: false},
			},
		}
	}

	if summary.CurrentGame == nil {
		return &discordgo.MessageEmbed{
			Title:       "🎮 Deadlock Current Game • " + summary.Name,
			Description: deadlockSummaryDescription(summary, ""),
			Color:       deadlockEmbedColor(summary),
			Fields:      []*discordgo.MessageEmbedField{{Name: "Status", Value: "Current game was not requested.", Inline: false}},
		}
	}

	return buildDeadlockCurrentGameEmbed(*summary.CurrentGame)
}

func buildDeadlockCurrentGameEmbed(status deadlockapi.CurrentGameStatus) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}
	color := deadlockColorNeutral

	if !status.InGame || status.Match == nil || status.Player == nil {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Status",
			Value:  "⚫ No active match found for this player in the Deadlock API watch data.",
			Inline: false,
		})
	} else {
		color = deadlockColorGreen
		match := status.Match
		player := status.Player
		hero := formatCurrentHero(player)

		fields = append(fields,
			&discordgo.MessageEmbedField{Name: "🟢 Status", Value: "**Currently in game**", Inline: true},
			&discordgo.MessageEmbedField{Name: "👤 Player", Value: formatActivePlayerName(player), Inline: true},
			&discordgo.MessageEmbedField{Name: "#️⃣ Match ID", Value: formatOptionalInt64(match.MatchID), Inline: true},
			&discordgo.MessageEmbedField{Name: "🎭 Hero", Value: hero, Inline: true},
			&discordgo.MessageEmbedField{Name: "🛡️ Team", Value: formatTeam(player), Inline: true},
			&discordgo.MessageEmbedField{Name: "⏱️ Duration", Value: formatDuration(match.DurationS), Inline: true},
			&discordgo.MessageEmbedField{Name: "💰 Team Souls", Value: formatTeamSouls(match, player), Inline: true},
			&discordgo.MessageEmbedField{Name: "🎮 Mode", Value: formatParsedOrUnknown(match.MatchModeParsed), Inline: true},
			&discordgo.MessageEmbedField{Name: "🌍 Region", Value: formatParsedOrUnknown(match.RegionModeParsed), Inline: true},
			&discordgo.MessageEmbedField{Name: "👀 Spectators", Value: formatOptionalInt32(match.Spectators), Inline: true},
		)

		roster := formatActiveRoster(match)
		if roster != "" {
			fields = append(fields, &discordgo.MessageEmbedField{Name: "Players In Match", Value: roster, Inline: false})
		}
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🎮 Deadlock Current Game • " + status.Name,
		Description: fmt.Sprintf("Steam account ID: `%d`", status.AccountID),
		URL:         status.ProfileURL,
		Color:       color,
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Use Refresh to re-check active match status."},
	}

	if status.Player != nil && status.Player.HeroIconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Player.HeroIconURL}
	} else if status.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: status.Avatar}
	}

	return embed
}

func buildDeadlockBuildsEmbed(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbed {
	fields := []*discordgo.MessageEmbedField{}

	if summary.BuildError != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "🧰 Builds & Items", Value: "Unavailable: " + summary.BuildError, Inline: false})
	} else if summary.Build == nil {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "🧰 Builds & Items", Value: "Build stats were not requested.", Inline: false})
	} else {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "🎭 Hero",
			Value:  fmt.Sprintf("**%s**\nHero ID `%d`", summary.Build.HeroName, summary.Build.HeroID),
			Inline: true,
		})
		fields = append(fields, &discordgo.MessageEmbedField{Name: "📚 Source", Value: summary.Build.BuildsSourceNote, Inline: true})

		buildLines := []string{}
		for index, build := range summary.Build.Builds {
			buildLines = append(buildLines, fmt.Sprintf(
				"`#%d` **Build %d** — %s matches • **%.1f%% WR** • %s players",
				index+1,
				build.HeroBuildID,
				formatInt(int(build.Matches)),
				build.WinRate,
				formatInt(int(build.Players)),
			))
		}
		fields = append(fields, &discordgo.MessageEmbedField{Name: "🏗️ Top Builds", Value: emptyFallback(strings.Join(buildLines, "\n"), "No build rows returned."), Inline: false})

		itemLines := []string{}
		for index, item := range summary.Build.PopularItems {
			itemLines = append(itemLines, fmt.Sprintf("`#%d` **%s** • %s builds", index+1, item.Name, formatInt(int(item.Builds))))
		}
		fields = append(fields, &discordgo.MessageEmbedField{Name: "🔥 Popular Items", Value: emptyFallback(strings.Join(itemLines, "\n"), "No item rows returned."), Inline: false})
	}

	embed := &discordgo.MessageEmbed{
		Title:       "🧰 Builds & Items • " + summary.Name,
		Description: deadlockSummaryDescription(summary, "Build IDs come from Deadlock API hero build analytics."),
		URL:         summary.ProfileURL,
		Color:       deadlockEmbedColor(summary),
		Fields:      fields,
		Footer:      &discordgo.MessageEmbedFooter{Text: "Build IDs come from Deadlock API hero build analytics."},
	}

	if summary.Build != nil && summary.Build.HeroIconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Build.HeroIconURL}
	} else if summary.TopHeroIconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.TopHeroIconURL}
	} else if summary.Avatar != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: summary.Avatar}
	}

	return embed
}

func buildDeadlockErrorEmbed(profile deadlockapi.SteamProfile, err error) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title:       "⚠️ Deadlock Statistics Error",
		Description: fmt.Sprintf("Could not refresh stats for `%s`: %v", profile.DisplayName(), err),
		Color:       deadlockColorRed,
	}
}

func rankFieldFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbedField {
	value := "Rank was not requested."
	if summary.Rank != nil {
		value = fmt.Sprintf(
			"**%s**\nBadge `%d` • Raw score `%.1f`\n`%d` matches used",
			summary.Rank.DisplayName(),
			summary.Rank.Badge,
			summary.Rank.RawScore,
			summary.Rank.MatchesUsed,
		)
	} else if summary.RankError != "" {
		value = "Unavailable: " + summary.RankError
	}

	return &discordgo.MessageEmbedField{Name: "🏅 Rank Prediction", Value: value, Inline: false}
}

func currentGameFieldFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbedField {
	value := "Current game was not requested."
	if summary.CurrentGame != nil {
		if summary.CurrentGame.InGame {
			value = "🟢 **Currently in game**"
			if summary.CurrentGame.Match != nil && summary.CurrentGame.Match.MatchID != nil {
				value += fmt.Sprintf(" • Match `%d`", *summary.CurrentGame.Match.MatchID)
			}
			if summary.CurrentGame.Player != nil {
				value += " • " + formatCurrentHero(summary.CurrentGame.Player)
			}
			if summary.CurrentGame.Match != nil {
				value += " • " + formatDuration(summary.CurrentGame.Match.DurationS)
			}
		} else {
			value = "⚫ No active match found for this player."
		}
	} else if summary.CurrentGameError != "" {
		value = "Unavailable: " + summary.CurrentGameError
	}

	return &discordgo.MessageEmbedField{Name: "🎮 Current Game", Value: value, Inline: false}
}

func buildFieldFromSummary(summary deadlockapi.PlayerSummary) *discordgo.MessageEmbedField {
	value := "Build stats were not requested."
	if summary.Build != nil {
		value = fmt.Sprintf("**%s** • %d build rows • %d popular items\n%s", summary.Build.HeroName, len(summary.Build.Builds), len(summary.Build.PopularItems), summary.Build.BuildsSourceNote)
	} else if summary.BuildError != "" {
		value = "Unavailable: " + summary.BuildError
	}
	return &discordgo.MessageEmbedField{Name: "🧰 Builds & Items", Value: value, Inline: false}
}

func formatRecentDeadlockMatches(matches []deadlockapi.RecentMatch) string {
	var lines []string

	for _, match := range matches {
		result := "🔴 Loss"
		if match.Won {
			result = "🟢 Win"
		}

		started := ""
		if match.StartedUnix > 0 {
			started = fmt.Sprintf(" • <t:%d:R>", match.StartedUnix)
		}

		hero := fmt.Sprintf("Hero `%d`", match.HeroID)
		if match.HeroName != "" {
			hero = fmt.Sprintf("**%s**", match.HeroName)
		}

		lines = append(lines, fmt.Sprintf(
			"%s • %s • `%d/%d/%d` • %s souls • %dm%s\n↳ Match `%d`",
			result,
			hero,
			match.Kills,
			match.Deaths,
			match.Assists,
			formatInt(int(match.NetWorth)),
			match.DurationMins,
			started,
			match.MatchID,
		))
	}

	return truncateDiscordField(strings.Join(lines, "\n"))
}

func formatActiveRoster(match *deadlockapi.ActiveMatch) string {
	if match == nil || len(match.Players) == 0 {
		return ""
	}

	team0 := []string{}
	team1 := []string{}
	unknown := []string{}

	for _, player := range match.Players {
		line := formatActiveRosterPlayer(player)
		switch {
		case player.Team != nil && *player.Team == 0:
			team0 = append(team0, line)
		case player.Team != nil && *player.Team == 1:
			team1 = append(team1, line)
		default:
			unknown = append(unknown, line)
		}
	}

	sections := []string{}
	if len(team0) > 0 {
		sections = append(sections, "**Team 0**\n"+strings.Join(team0[:minInt(len(team0), 6)], "\n"))
	}
	if len(team1) > 0 {
		sections = append(sections, "**Team 1**\n"+strings.Join(team1[:minInt(len(team1), 6)], "\n"))
	}
	if len(unknown) > 0 {
		sections = append(sections, "**Unknown**\n"+strings.Join(unknown[:minInt(len(unknown), 4)], "\n"))
	}

	return truncateDiscordField(strings.Join(sections, "\n"))
}

func formatActiveRosterPlayer(player deadlockapi.ActiveMatchPlayer) string {
	playerName := formatActivePlayerName(&player)
	hero := formatCurrentHero(&player)
	if player.Abandoned != nil && *player.Abandoned {
		return fmt.Sprintf("• %s — %s _(abandoned)_", playerName, hero)
	}
	return fmt.Sprintf("• %s — %s", playerName, hero)
}

func formatActivePlayerName(player *deadlockapi.ActiveMatchPlayer) string {
	if player == nil {
		return "Unknown"
	}

	account := "unknown"
	if player.AccountID != nil {
		account = strconv.FormatInt(*player.AccountID, 10)
	}

	name := strings.TrimSpace(player.DisplayName)
	if name != "" && !strings.EqualFold(name, "Unknown Player") {
		return fmt.Sprintf("**%s** (`%s`)", name, account)
	}

	return "`" + account + "`"
}

func formatCurrentHero(player *deadlockapi.ActiveMatchPlayer) string {
	if player == nil {
		return "Unknown"
	}
	if player.HeroName != "" {
		if player.HeroID != nil {
			return fmt.Sprintf("**%s** (`%d`)", player.HeroName, *player.HeroID)
		}
		return "**" + player.HeroName + "**"
	}
	return formatOptionalInt32(player.HeroID)
}

func buildDeadlockComponents(state deadlockComponentState, recentCount int) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			deadlockButton("📊 Overview", deadlockViewOverview, state, discordgo.SecondaryButton, 0),
			deadlockButton("🏅 Rank", deadlockViewRank, state, discordgo.SecondaryButton, 0),
			deadlockButton("🕘 Recent", deadlockViewRecent, state, discordgo.SecondaryButton, state.Page),
			deadlockButton("🎮 Current", deadlockViewCurrent, state, discordgo.SecondaryButton, 0),
			deadlockButton("🧰 Builds", deadlockViewBuilds, state, discordgo.SecondaryButton, 0),
		}},
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			deadlockButton("✨ All", deadlockViewAll, state, discordgo.SecondaryButton, 0),
			deadlockPageButton("◀ Recent", state, -1, recentCount),
			deadlockPageButton("Recent ▶", state, 1, recentCount),
			deadlockButton("🔄 Refresh", state.View, state, discordgo.PrimaryButton, state.Page),
		}},
	}
}

func deadlockButton(label string, view string, state deadlockComponentState, style discordgo.ButtonStyle, page int) discordgo.Button {
	if state.View == view {
		style = discordgo.PrimaryButton
	}
	state.View = view
	state.Page = page
	action := "tab_" + cleanDeadlockView(view)
	if strings.Contains(strings.ToLower(label), "refresh") {
		action = "refresh"
	}

	return discordgo.Button{Label: label, Style: style, CustomID: deadlockCustomIDWithAction(state, action)}
}

func deadlockPageButton(label string, state deadlockComponentState, delta int, recentCount int) discordgo.Button {
	state.View = deadlockViewRecent
	state.Page += delta
	state.Page = clampRecentPage(state.Page, recentCount)

	disabled := recentCount <= deadlockRecentPageSize
	if delta < 0 && state.Page <= 0 {
		disabled = true
	}
	maxPage := 0
	if recentCount > 0 {
		maxPage = (recentCount - 1) / deadlockRecentPageSize
	}
	if delta > 0 && state.Page >= maxPage {
		disabled = true
	}

	action := "page_next"
	if delta < 0 {
		action = "page_prev"
	}

	return discordgo.Button{Label: label, Style: discordgo.SecondaryButton, CustomID: deadlockCustomIDWithAction(state, action), Disabled: disabled}
}

func deadlockCustomID(state deadlockComponentState) string {
	return deadlockCustomIDWithAction(state, "open")
}

func deadlockCustomIDWithAction(state deadlockComponentState, action string) string {
	action = strings.ReplaceAll(strings.TrimSpace(strings.ToLower(action)), ":", "_")
	if action == "" {
		action = "open"
	}

	return fmt.Sprintf(
		"%s:stats:%s:%d:%s:%d:%d:%t:%s",
		deadlockComponentPrefix,
		state.OwnerID,
		state.AccountID,
		cleanDeadlockView(state.View),
		state.Page,
		state.RecentLimit,
		state.UseRankImage,
		action,
	)
}

func parseDeadlockComponentState(customID string) (deadlockComponentState, error) {
	parts := strings.Split(customID, ":")
	if (len(parts) != 8 && len(parts) != 9) || parts[0] != deadlockComponentPrefix || parts[1] != "stats" {
		return deadlockComponentState{}, fmt.Errorf("invalid custom id")
	}

	accountID, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return deadlockComponentState{}, err
	}
	page, err := strconv.Atoi(parts[5])
	if err != nil {
		return deadlockComponentState{}, err
	}
	recentLimit, err := strconv.Atoi(parts[6])
	if err != nil {
		return deadlockComponentState{}, err
	}
	useRankImage, err := strconv.ParseBool(parts[7])
	if err != nil {
		return deadlockComponentState{}, err
	}

	return deadlockComponentState{
		OwnerID:      parts[2],
		AccountID:    accountID,
		View:         cleanDeadlockView(parts[4]),
		Page:         page,
		RecentLimit:  clampInt(recentLimit, 1, maxDeadlockRecentLimit),
		UseRankImage: useRankImage,
	}, nil
}

func (d *DeadlockStatistics) storeSession(ownerID string, profile deadlockapi.SteamProfile, recentLimit int, useRankImage bool) {
	if ownerID == "" || profile.AccountID <= 0 {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.sessions[deadlockSessionKey(ownerID, profile.AccountID)] = deadlockSession{
		Profile:      profile,
		RecentLimit:  recentLimit,
		UseRankImage: useRankImage,
		UpdatedAt:    time.Now(),
	}
}

func (d *DeadlockStatistics) sessionProfile(state deadlockComponentState) deadlockapi.SteamProfile {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := deadlockSessionKey(state.OwnerID, state.AccountID)
	if session, ok := d.sessions[key]; ok {
		session.UpdatedAt = time.Now()
		d.sessions[key] = session
		return session.Profile
	}

	return deadlockapi.SteamProfile{AccountID: state.AccountID}
}

func deadlockSessionKey(ownerID string, accountID int64) string {
	return ownerID + ":" + strconv.FormatInt(accountID, 10)
}

func cleanDeadlockView(view string) string {
	switch strings.TrimSpace(strings.ToLower(view)) {
	case deadlockViewRank:
		return deadlockViewRank
	case deadlockViewRecent:
		return deadlockViewRecent
	case deadlockViewCurrent:
		return deadlockViewCurrent
	case deadlockViewBuilds:
		return deadlockViewBuilds
	case deadlockViewAll:
		return deadlockViewAll
	default:
		return deadlockViewOverview
	}
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i == nil || i.Interaction == nil {
		return ""
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func deadlockSummaryDescription(summary deadlockapi.PlayerSummary, note string) string {
	parts := []string{
		fmt.Sprintf("Steam account ID: `%d`", summary.AccountID),
		fmt.Sprintf("**%s matches** • **%.1f%% WR** • %s", formatInt(summary.Matches), summary.WinRate, formatRecordDifferential(summary.Wins, summary.Losses)),
	}
	if strings.TrimSpace(note) != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, "\n")
}

func deadlockEmbedColor(summary deadlockapi.PlayerSummary) int {
	if summary.Matches <= 0 {
		return deadlockColorNeutral
	}

	switch {
	case summary.WinRate >= 55:
		return deadlockColorGreen
	case summary.WinRate < 45:
		return deadlockColorRed
	default:
		return deadlockColorGold
	}
}

func formatRecordDifferential(wins int, losses int) string {
	diff := wins - losses
	switch {
	case diff > 0:
		return fmt.Sprintf("`+%d` game differential", diff)
	case diff < 0:
		return fmt.Sprintf("`%d` game differential", diff)
	default:
		return "`Even` record"
	}
}

func formatWinRateLabel(winRate float64, matches int) string {
	if matches <= 0 {
		return "No completed games"
	}

	switch {
	case winRate >= 60:
		return "🔥 dominant form"
	case winRate >= 55:
		return "🟢 winning form"
	case winRate >= 50:
		return "🟡 positive form"
	default:
		return "🔴 needs momentum"
	}
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

func emptyFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func truncateDiscordField(value string) string {
	const maxFieldLength = 1024
	runes := []rune(value)
	if len(runes) <= maxFieldLength {
		return value
	}
	return string(runes[:maxFieldLength-1]) + "…"
}

func clampRecentPage(page int, recentCount int) int {
	if page < 0 {
		return 0
	}
	if recentCount <= 0 {
		return 0
	}
	maxPage := (recentCount - 1) / deadlockRecentPageSize
	if page > maxPage {
		return maxPage
	}
	return page
}

func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func floatPtr(value float64) *float64 {
	return &value
}
