package commands

import (
	"fmt"
	"strings"

	"discord_gobot/internal/discordutil"
	"discord_gobot/internal/weather"

	"github.com/bwmarrin/discordgo"
)

// Weather implements the /weather command.
//
// Examples:
//
//	/weather location:Dublin
//	/weather location:Dublin view:today
//	/weather location:Dublin view:two_days
//	/weather location:Dublin view:full private:true
type Weather struct{}

func (Weather) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "weather",
		Description: "Get weather from wttr.in",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "location",
				Description: "City, town, airport code, or place name",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "view",
				Description: "How much weather information to show",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{
						Name:  "Compact one-line",
						Value: "compact",
					},
					{
						Name:  "Current weather only",
						Value: "current",
					},
					{
						Name:  "Today forecast",
						Value: "today",
					},
					{
						Name:  "Today and tomorrow",
						Value: "two_days",
					},
					{
						Name:  "Full wttr.in forecast",
						Value: "full",
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "units",
				Description: "Weather units",
				Required:    false,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{
						Name:  "Metric: °C and km/h",
						Value: "m",
					},
					{
						Name:  "US: °F and mph",
						Value: "u",
					},
					{
						Name:  "Metric: °C and m/s wind",
						Value: "M",
					},
				},
			},
			{
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Name:        "private",
				Description: "Only show the weather result to you",
				Required:    false,
			},
		},
	}
}

func (Weather) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	options := discordutil.OptionMap(i.ApplicationCommandData().Options)

	locationOption, ok := options["location"]
	if !ok {
		discordutil.Respond(s, i, "Missing required input: location", true)
		return
	}

	location := strings.TrimSpace(locationOption.StringValue())
	if location == "" {
		discordutil.Respond(s, i, "Location cannot be empty.", true)
		return
	}

	view := "today"
	if viewOption, ok := options["view"]; ok {
		view = viewOption.StringValue()
	}

	units := "m"
	if unitsOption, ok := options["units"]; ok {
		units = unitsOption.StringValue()
	}

	private := false
	if privateOption, ok := options["private"]; ok {
		private = privateOption.BoolValue()
	}

	discordutil.Defer(s, i, private)

	result, err := weather.FetchWttr(location, units, view)
	if err != nil {
		discordutil.EditOriginal(
			s,
			i,
			fmt.Sprintf("Could not get weather for `%s`: %v", location, err),
		)
		return
	}

	sendWeatherOutput(s, i, result, private)
}

func sendWeatherOutput(s *discordgo.Session, i *discordgo.InteractionCreate, output string, private bool) {
	chunks := splitForDiscordCodeBlocks(output)

	if len(chunks) == 0 {
		discordutil.EditOriginal(s, i, "wttr.in returned no weather output.")
		return
	}

	discordutil.EditOriginal(s, i, codeBlock(chunks[0]))

	for _, chunk := range chunks[1:] {
		discordutil.FollowUp(s, i, codeBlock(chunk), private)
	}
}

func codeBlock(value string) string {
	// Prevent accidental closing of our code block if the response ever
	// contains triple backticks.
	value = strings.ReplaceAll(value, "```", "`\u200b``")

	return "```text\n" + value + "\n```"
}

func splitForDiscordCodeBlocks(value string) []string {
	// Leave room for:
	//
	//   ```text
	//   ...
	//   ```
	//
	// Discord messages max out at 2000 characters, so 1800 gives us a safe
	// buffer and avoids edge cases with Unicode/weather symbols.
	const maxRunes = 1800

	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	var chunks []string
	var current []rune

	for _, line := range strings.Split(value, "\n") {
		lineRunes := []rune(line)

		if len(current)+len(lineRunes)+1 > maxRunes && len(current) > 0 {
			chunks = append(chunks, strings.TrimRight(string(current), "\n"))
			current = nil
		}

		for len(lineRunes) > maxRunes {
			chunks = append(chunks, string(lineRunes[:maxRunes]))
			lineRunes = lineRunes[maxRunes:]
		}

		current = append(current, lineRunes...)
		current = append(current, '\n')
	}

	if len(current) > 0 {
		chunks = append(chunks, strings.TrimRight(string(current), "\n"))
	}

	return chunks
}
