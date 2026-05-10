package commands

import (
	"fmt"
	"strings"

	"discord_gobot/internal/discordutil"
	"discord_gobot/internal/weather"

	"github.com/bwmarrin/discordgo"
)

// Weather implements the /weather command
//
// Example:
//
//	/weather location:Dublin
//	/weather location:New York units:us
//	/weather location:Tokyo private:true
type Weather struct{}

// Definition tells Discord what the /weather command looks like.
func (Weather) Definition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "weather",
		Description: "Get the current weather from wttr.in",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "location",
				Description: "City, town, airport code, or place name",
				Required:    true,
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

// Handle runs when the user executes /weather
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

	units := "m"
	if unitsOption, ok := options["units"]; ok {
		units = unitsOption.StringValue()
	}

	private := false
	if privateOption, ok := options["private"]; ok {
		private = privateOption.BoolValue()
	}

	// Weather calls can take longer than Discord's immediate response window,
	// so we defer first, then edit the response after curl finishes
	discordutil.Defer(s, i, private)

	result, err := weather.FetchWttr(location, units)
	if err != nil {
		discordutil.EditOriginal(
			s,
			i,
			fmt.Sprintf("Could not get weather for `%s`: %v", location, err),
		)
		return
	}

	message := fmt.Sprintf("```text\n%s\n```", limitDiscordMessage(result))

	discordutil.EditOriginal(s, i, message)
}

// limitDiscordMessage keeps the response safely under Discord's message limit.
//
// Discord message content supports up to 2000 characters, and we also wrap the
// response in a code block, so the weather text needs to leave room for that
func limitDiscordMessage(value string) string {
	const maxWeatherLength = 1900

	value = strings.TrimSpace(value)

	if len(value) <= maxWeatherLength {
		return value
	}

	return value[:maxWeatherLength] + "\n...weather output truncated..."
}
