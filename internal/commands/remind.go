package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"discord_gobot/internal/discordutil"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

const (
	remindCancelPrefix  = "remind:cancel:"
	maxReminderAhead    = 365 * 24 * time.Hour
	maxRemindersPerUser = 25
)

// Remind implements /remind: pings a member later, in the channel the
// reminder was set in. Reminders are saved to a JSON file so they survive
// restarts; ones that came due while the bot was down go off at startup.
type Remind struct {
	path string

	mu        sync.Mutex
	client    *bot.Client
	reminders map[string]reminder
	timers    map[string]*time.Timer
}

type reminder struct {
	ID        string       `json:"id"`
	ChannelID snowflake.ID `json:"channel_id"`
	CreatorID snowflake.ID `json:"creator_id"`
	TargetID  snowflake.ID `json:"target_id"`
	Message   string       `json:"message"`
	Due       time.Time    `json:"due"`
}

func NewRemind(path string) *Remind {
	return &Remind{path: path, reminders: make(map[string]reminder), timers: make(map[string]*time.Timer)}
}

func (r *Remind) ComponentPrefix() string { return "remind" }

func (r *Remind) Definition() discord.SlashCommandCreate {
	return discord.SlashCommandCreate{
		Name:        "remind",
		Description: "Ping someone (or yourself) in this channel later",
		Contexts:    []discord.InteractionContextType{discord.InteractionContextTypeGuild},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionString{
				Name:        "when",
				Description: "In how long, e.g. 20m, 1h30m, 2 days, or a time like 18:30 or 6pm",
				Required:    true,
			},
			discord.ApplicationCommandOptionString{
				Name:        "what",
				Description: "What to remind them about",
				Required:    true,
				MaxLength:   new(500),
			},
			discord.ApplicationCommandOptionUser{
				Name:        "who",
				Description: "Who to remind. Defaults to you.",
			},
		},
	}
}

// OnEvent loads saved reminders once the bot has connected.
func (r *Remind) OnEvent(event bot.Event) {
	ready, ok := event.(*events.Ready)
	if !ok {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil {
		return // Ready fires again after a full reconnect
	}
	r.client = ready.Client()

	saved, err := loadReminders(r.path)
	if err != nil {
		log.Printf("remind: load %s: %v", r.path, err)
	}
	for _, rem := range saved {
		r.reminders[rem.ID] = rem
		r.scheduleLocked(rem)
	}
	if len(saved) > 0 {
		log.Printf("remind: loaded %d reminders", len(saved))
	}
}

func (r *Remind) Handle(e *events.ApplicationCommandInteractionCreate) {
	data := e.SlashCommandInteractionData()

	when := data.String("when")
	due, ok := parseWhen(when, time.Now())
	if !ok {
		discordutil.Reply(e, fmt.Sprintf("I couldn't read `%s` as a time. Try 20m, 1h30m, 2 days, 18:30 or 6pm.", when), true)
		return
	}
	if time.Until(due) > maxReminderAhead {
		discordutil.Reply(e, "That's more than a year away.", true)
		return
	}

	targetID, ok := data.OptSnowflake("who")
	if !ok {
		targetID = e.User().ID
	}
	rem := reminder{
		ID:        randomID(),
		ChannelID: e.Channel().ID(),
		CreatorID: e.User().ID,
		TargetID:  targetID,
		Message:   strings.TrimSpace(data.String("what")),
		Due:       due,
	}

	if problem := r.add(rem); problem != "" {
		discordutil.Reply(e, problem, true)
		return
	}

	err := e.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("⏰ I'll remind <@%s> <t:%d:R> (<t:%d:f>): %s", targetID, due.Unix(), due.Unix(), rem.Message),
		Components: []discord.LayoutComponent{
			discord.NewActionRow(discord.NewSecondaryButton("Cancel reminder", remindCancelPrefix+rem.ID)),
		},
	})
	if err != nil {
		// They saw the command fail and will likely retry; don't keep a duplicate
		log.Printf("remind: confirm %s: %v", rem.ID, err)
		r.cancel(rem.ID, rem.CreatorID)
	}
}

func (r *Remind) HandleComponent(e *events.ComponentInteractionCreate) {
	id, _ := strings.CutPrefix(e.Data.CustomID(), remindCancelPrefix)

	rem, found, allowed := r.cancel(id, e.User().ID)
	switch {
	case !found:
		discordutil.Reply(e, "That reminder already went off or was cancelled.", true)
	case !allowed:
		discordutil.Reply(e, "Only the person who set this reminder, or the person it's for, can cancel it.", true)
	default:
		content := fmt.Sprintf("~~⏰ Reminder for <@%s>: %s~~\nCancelled by <@%s>.", rem.TargetID, rem.Message, e.User().ID)
		if err := e.UpdateMessage(discord.MessageUpdate{Content: &content, Components: &[]discord.LayoutComponent{}}); err != nil {
			log.Printf("remind: update cancelled %s: %v", id, err)
		}
	}
}

// add saves and schedules a reminder, or returns why it can't.
func (r *Remind) add(rem reminder) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.client == nil {
		return "Reminders aren't ready yet; try again in a moment."
	}
	pending := 0
	for _, existing := range r.reminders {
		if existing.CreatorID == rem.CreatorID {
			pending++
		}
	}
	if pending >= maxRemindersPerUser {
		return fmt.Sprintf("You already have %d reminders waiting. Cancel one first.", maxRemindersPerUser)
	}

	r.reminders[rem.ID] = rem
	if err := r.saveLocked(); err != nil {
		delete(r.reminders, rem.ID)
		log.Printf("remind: save: %v", err)
		return "Couldn't save the reminder."
	}
	r.scheduleLocked(rem)
	return ""
}

// cancel removes a reminder if userID set it or is its target.
func (r *Remind) cancel(id string, userID snowflake.ID) (rem reminder, found bool, allowed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rem, found = r.reminders[id]
	if !found || (userID != rem.CreatorID && userID != rem.TargetID) {
		return rem, found, false
	}
	r.removeLocked(id)
	return rem, true, true
}

func (r *Remind) fire(id string) {
	r.mu.Lock()
	rem, ok := r.reminders[id]
	if ok {
		r.removeLocked(id)
	}
	client := r.client
	r.mu.Unlock()
	if !ok {
		return
	}

	text := fmt.Sprintf("⏰ <@%s> reminder: %s", rem.TargetID, rem.Message)
	if rem.CreatorID != rem.TargetID {
		text = fmt.Sprintf("⏰ <@%s>, <@%s> asked me to remind you: %s", rem.TargetID, rem.CreatorID, rem.Message)
	}
	if time.Since(rem.Due) > time.Minute {
		text += fmt.Sprintf("\n-# This was due <t:%d:R>, while I was offline.", rem.Due.Unix())
	}

	// Ping only the person being reminded, never roles or @everyone in the text
	_, err := client.Rest.CreateMessage(rem.ChannelID, discord.MessageCreate{
		Content:         text,
		AllowedMentions: &discord.AllowedMentions{Parse: []discord.AllowedMentionType{}, Users: []snowflake.ID{rem.TargetID}},
	})
	if err != nil {
		log.Printf("remind: send %s: %v", id, err)
	}
}

func (r *Remind) scheduleLocked(rem reminder) {
	// A due time in the past fires straight away
	r.timers[rem.ID] = time.AfterFunc(time.Until(rem.Due), func() { r.fire(rem.ID) })
}

func (r *Remind) removeLocked(id string) {
	if timer := r.timers[id]; timer != nil {
		timer.Stop()
	}
	delete(r.timers, id)
	delete(r.reminders, id)
	if err := r.saveLocked(); err != nil {
		log.Printf("remind: save: %v", err)
	}
}

// saveLocked writes all reminders, replacing the file atomically so a crash
// mid-write can't corrupt it.
func (r *Remind) saveLocked() error {
	list := make([]reminder, 0, len(r.reminders))
	for _, rem := range r.reminders {
		list = append(list, rem)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(r.path+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(r.path+".tmp", r.path)
}

func loadReminders(path string) ([]reminder, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []reminder
	return list, json.Unmarshal(data, &list)
}

// timeWords turns "1 hour and 30 minutes" into "1h30m" for time.ParseDuration.
// Longer words come first so "mins" wins over "min".
var timeWords = strings.NewReplacer(
	"minutes", "m", "minute", "m", "mins", "m", "min", "m",
	"hours", "h", "hour", "h", "hrs", "h", "hr", "h",
	"days", "d", "day", "d",
	"seconds", "s", "second", "s", "secs", "s", "sec", "s",
	"and", "", " ", "", ",", "",
)

// parseWhen reads a delay ("20m", "1h30m", "2 days") or a clock time
// ("18:30", "6pm", "6:30pm") as the next time after now, in now's time zone.
func parseWhen(input string, now time.Time) (time.Time, bool) {
	s := strings.ToLower(strings.TrimSpace(input))

	compact := strings.ReplaceAll(s, " ", "")
	for _, layout := range []string{"15:04", "3:04pm", "3pm"} {
		if clock, err := time.Parse(layout, compact); err == nil {
			due := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
			if !due.After(now) {
				due = due.AddDate(0, 0, 1)
			}
			return due, true
		}
	}

	s = timeWords.Replace(s)
	var total time.Duration
	if days, rest, ok := strings.Cut(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return time.Time{}, false
		}
		total, s = time.Duration(n)*24*time.Hour, rest
	}
	if s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return time.Time{}, false
		}
		total += d
	}
	if total <= 0 {
		return time.Time{}, false
	}
	return now.Add(total), true
}
