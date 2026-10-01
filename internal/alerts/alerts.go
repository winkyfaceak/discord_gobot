// Package alerts forwards home lab alerts from an ntfy topic to the bot
// owner's Discord DMs.
package alerts

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

type message struct {
	ID      string `json:"id"`
	Event   string `json:"event"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Click   string `json:"click"`
}

// Forward DMs every message on the ntfy topic to the owner of the Discord
// app until ctx is cancelled. After a dropped connection it resumes from the
// last alert, so nothing sent in between is lost (ntfy caches them).
func Forward(ctx context.Context, client *bot.Client, ntfyURL, topic, token string) {
	var dmID snowflake.ID
	since := strconv.FormatInt(time.Now().Unix(), 10)

	for {
		var err error
		if dmID == 0 {
			dmID, err = ownerDM(client)
		}
		if err == nil {
			err = subscribe(ctx, ntfyURL+"/"+topic+"/json?since="+since, token, func(m message) {
				since = m.ID
				if _, err := client.Rest.CreateMessage(dmID, discord.MessageCreate{Content: format(m)}); err != nil {
					log.Printf("alerts: DM %q: %v", m.Title, err)
				}
			})
		}
		if ctx.Err() != nil {
			return
		}
		log.Printf("alerts: %v; retrying in 10s", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

// ownerDM opens a DM with whoever owns the Discord app.
func ownerDM(client *bot.Client) (snowflake.ID, error) {
	app, err := client.Rest.GetBotApplicationInfo()
	if err != nil {
		return 0, fmt.Errorf("look up app owner: %w", err)
	}
	if app.Owner == nil {
		return 0, fmt.Errorf("app has no owner to DM")
	}
	dm, err := client.Rest.CreateDMChannel(app.Owner.ID)
	if err != nil {
		return 0, fmt.Errorf("open DM with owner: %w", err)
	}
	return dm.ID(), nil
}

// subscribe streams ntfy's JSON lines, calling handle for each message
// (skipping open/keepalive events), until the stream ends.
func subscribe(ctx context.Context, url, token string, handle func(message)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	// No client timeout: the stream is meant to stay open
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to ntfy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ntfy returned %s", resp.Status)
	}

	lines := bufio.NewScanner(resp.Body)
	lines.Buffer(make([]byte, 64<<10), 1<<20)
	for lines.Scan() {
		var m message
		if json.Unmarshal(lines.Bytes(), &m) == nil && m.Event == "message" {
			handle(m)
		}
	}
	if err := lines.Err(); err != nil {
		return fmt.Errorf("read ntfy stream: %w", err)
	}
	return fmt.Errorf("ntfy stream closed: %w", io.EOF)
}

// format renders an alert as a Discord message (2000 characters max).
func format(m message) string {
	text := m.Message
	if m.Title != "" {
		text = "**" + m.Title + "**\n" + text
	}
	if m.Click != "" {
		text += "\n" + m.Click
	}
	if runes := []rune(text); len(runes) > 2000 {
		text = string(runes[:1999]) + "…"
	}
	return text
}
