package commands

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// TimeZones implements /timezone and the "Convert times" message command:
// right-click a message and see the times in it in your own time zone.
// Times are read in the author's zone (set with /timezone) and shown as
// Discord timestamps, which every reader's app displays in their local time.
type TimeZones struct {
	path  string
	zones []string // known zone names, for autocomplete

	mu     sync.Mutex
	byUser map[snowflake.ID]string
}

func NewTimeZones(path string) *TimeZones {
	t := &TimeZones{path: path, byUser: make(map[snowflake.ID]string), zones: zoneNames()}
	if err := loadJSON(path, &t.byUser); err != nil {
		log.Printf("timezone: load %s: %v", path, err)
	}
	return t
}

func (t *TimeZones) Commands() []Command {
	return []Command{
		funcCommand{
			def: discord.SlashCommandCreate{
				Name:        "timezone",
				Description: "Set your time zone, so others can convert the times you write",
				Options: []discord.ApplicationCommandOption{
					discord.ApplicationCommandOptionString{
						Name:         "zone",
						Description:  "Your city's zone, e.g. Europe/London or America/New_York",
						Required:     true,
						Autocomplete: true,
					},
				},
			},
			handle:       t.set,
			autocomplete: t.suggest,
		},
		funcCommand{def: discord.MessageCommandCreate{Name: "Convert times"}, handle: t.convert},
	}
}

func (t *TimeZones) set(e *events.ApplicationCommandInteractionCreate) {
	name := strings.TrimSpace(e.SlashCommandInteractionData().String("zone"))
	loc, err := time.LoadLocation(name)
	if err != nil || name == "" || name == "Local" {
		discordutil.Reply(e, fmt.Sprintf("I don't know the time zone `%s`. Pick one from the list, like Europe/London.", name), true)
		return
	}

	t.mu.Lock()
	t.byUser[e.User().ID] = loc.String()
	err = saveJSON(t.path, t.byUser)
	t.mu.Unlock()
	if err != nil {
		log.Printf("timezone: save: %v", err)
		discordutil.Reply(e, "Couldn't save your time zone.", true)
		return
	}
	discordutil.Reply(e, fmt.Sprintf("Saved: **%s** (it's %s there now). Anyone can now right-click your messages → Apps → **Convert times**.", loc, time.Now().In(loc).Format("15:04")), true)
}

// popularZones are suggested before anything is typed.
var popularZones = []string{
	"Europe/London", "Europe/Dublin", "Europe/Paris", "Europe/Berlin", "America/New_York",
	"America/Chicago", "America/Denver", "America/Los_Angeles", "Asia/Tokyo", "Australia/Sydney", "UTC",
}

func (t *TimeZones) suggest(e *events.AutocompleteInteractionCreate) {
	query := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(e.Data.String("zone")), " ", "_"))
	candidates := popularZones
	if query != "" {
		candidates = nil
		for _, zone := range t.zones {
			if strings.Contains(strings.ToLower(zone), query) {
				candidates = append(candidates, zone)
			}
		}
	}

	choices := []discord.AutocompleteChoice{}
	for _, zone := range candidates[:min(len(candidates), 25)] {
		choices = append(choices, discord.AutocompleteChoiceString{Name: zone, Value: zone})
	}
	if err := e.AutocompleteResult(choices); err != nil {
		log.Printf("timezone: autocomplete reply: %v", err)
	}
}

func (t *TimeZones) convert(e *events.ApplicationCommandInteractionCreate) {
	msg := e.MessageCommandInteractionData().TargetMessage()

	t.mu.Lock()
	zone, known := t.byUser[msg.Author.ID]
	t.mu.Unlock()
	loc := time.Local
	if known {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}

	found := findTimes(msg.Content, msg.CreatedAt.In(loc))
	if len(found) == 0 {
		discordutil.Reply(e, "I couldn't find any times like 9pm or 21:30 in that message.", true)
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Times in %s's message, in **your** time zone:\n", msg.Author.EffectiveName())
	for _, f := range found {
		fmt.Fprintf(&b, "**%s** → <t:%d:t> (<t:%d:R>)\n", f.text, f.at.Unix(), f.at.Unix())
	}
	if !known {
		fmt.Fprintf(&b, "-# %s hasn't set `/timezone`, so I assumed %s (the server's time zone).", msg.Author.EffectiveName(), msg.CreatedAt.In(loc).Format("MST"))
	}
	discordutil.Reply(e, b.String(), true)
}

type foundTime struct {
	text string
	at   time.Time
}

// clockTime matches 9pm, 9:30 pm and 21:30 (but not 1:2 or 123:45).
var clockTime = regexp.MustCompile(`(?i)\b(\d{1,2}(?::[0-5]\d)?\s?[ap]m|(?:[01]?\d|2[0-3]):[0-5]\d)\b`)

// findTimes returns the clock times in text, each as its next occurrence
// after sent (whose time zone is the writer's).
func findTimes(text string, sent time.Time) []foundTime {
	var found []foundTime
	seen := map[string]bool{}
	for _, match := range clockTime.FindAllString(text, -1) {
		if seen[strings.ToLower(match)] {
			continue
		}
		seen[strings.ToLower(match)] = true
		if at, ok := parseWhen(match, sent); ok {
			found = append(found, foundTime{text: match, at: at})
		}
	}
	return found
}

// zoneNames lists the system's time zones from zone.tab, for autocomplete.
func zoneNames() []string {
	dir := os.Getenv("TZDIR")
	if dir == "" {
		dir = "/usr/share/zoneinfo"
	}
	file, err := os.Open(filepath.Join(dir, "zone.tab"))
	if err != nil {
		log.Printf("timezone: list zones: %v", err)
		return popularZones
	}
	defer file.Close()

	zones := []string{"UTC"}
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) >= 3 && !strings.HasPrefix(fields[0], "#") {
			zones = append(zones, fields[2])
		}
	}
	slices.Sort(zones)
	return zones
}
