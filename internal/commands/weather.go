package commands

import (
	"fmt"
	"log"
	"strings"

	"discord_gobot/internal/discordutil"
	"discord_gobot/internal/weather"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
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

func (Weather) Definition() discord.ApplicationCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "weather",
		Description: "Get weather from wttr.in",
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionString{
				Name:        "location",
				Description: "City, town, airport code, or place name",
				Required:    true,
			},
			discord.ApplicationCommandOptionString{
				Name:        "view",
				Description: "How much weather information to show",
				Choices: []discord.ApplicationCommandOptionChoiceString{
					{Name: "Compact one-line", Value: "compact"},
					{Name: "Current weather only", Value: "current"},
					{Name: "Today forecast", Value: "today"},
					{Name: "Today and tomorrow", Value: "two_days"},
					{Name: "Full wttr.in forecast", Value: "full"},
				},
			},
			discord.ApplicationCommandOptionString{
				Name:        "units",
				Description: "Weather units",
				Choices: []discord.ApplicationCommandOptionChoiceString{
					{Name: "Metric: °C and km/h", Value: "m"},
					{Name: "US: °F and mph", Value: "u"},
					{Name: "Metric: °C and m/s wind", Value: "M"},
				},
			},
			discord.ApplicationCommandOptionBool{
				Name:        "private",
				Description: "Only show the weather result to you",
			},
		},
	}
}

func (Weather) Handle(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()

	location := strings.TrimSpace(data.String("location"))
	if location == "" {
		discordutil.Reply(e, "Location cannot be empty.", true)
		return
	}

	view, ok := data.OptString("view")
	if !ok {
		view = "today"
	}
	units, ok := data.OptString("units")
	if !ok {
		units = "m"
	}
	private := data.Bool("private")

	if err := e.DeferCreateMessage(private); err != nil {
		log.Printf("error deferring /weather: %v", err)
		return
	}

	result, err := weather.FetchWttr(location, units, view)
	if err != nil {
		discordutil.EditText(e, fmt.Sprintf("Could not get weather for `%s`: %v", location, err))
		return
	}

	chunks := splitForDiscordCodeBlocks(result)
	if len(chunks) == 0 {
		discordutil.EditText(e, "wttr.in returned no weather output.")
		return
	}

	discordutil.EditText(e, codeBlock(chunks[0]))
	for _, chunk := range chunks[1:] {
		discordutil.FollowUp(e, codeBlock(chunk), private)
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
