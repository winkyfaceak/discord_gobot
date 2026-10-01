package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/disgoorg/snowflake/v2"
)

// Config stores all values the bot needs from the outside world
//
// Keeping configuration in one struct is useful because the rest of the app
// does not need to call os.Getenv directly
type Config struct {
	// Token is your bot token from the Discord Developer Portal
	//
	// This should be the raw token only
	// Do not include "Bot " in the environment variable
	Token string

	// GuildID is optional
	//
	// If set, slash commands are registered only inside this server
	// This is much faster while developing
	//
	// If zero, slash commands are registered globally
	GuildID snowflake.ID

	// Navidrome login for /music. The music command is only registered when
	// NavidromeUser is set.
	NavidromeURL      string
	NavidromeUser     string
	NavidromePassword string

	// StateDir is where the bot keeps data across restarts (reminders).
	// systemd's StateDirectory= sets STATE_DIRECTORY; defaults to the
	// working directory.
	StateDir string
}

// Load reads configuration from environment variables
//
// Required: DISCORD_TOKEN
// Optional: GUILD_ID, NAVIDROME_URL (default http://127.0.0.1:4533),
// NAVIDROME_USER, NAVIDROME_PASSWORD, STATE_DIRECTORY
func Load() (Config, error) {
	cfg := Config{
		Token:             strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		NavidromeURL:      strings.TrimSpace(os.Getenv("NAVIDROME_URL")),
		NavidromeUser:     strings.TrimSpace(os.Getenv("NAVIDROME_USER")),
		NavidromePassword: os.Getenv("NAVIDROME_PASSWORD"),
		StateDir:          os.Getenv("STATE_DIRECTORY"),
	}

	// The bot cannot run without a token, so fail early with a clear message
	if cfg.Token == "" {
		return Config{}, errors.New("DISCORD_TOKEN is not set")
	}

	if guildID := strings.TrimSpace(os.Getenv("GUILD_ID")); guildID != "" {
		id, err := snowflake.Parse(guildID)
		if err != nil {
			return Config{}, fmt.Errorf("GUILD_ID is not a valid server ID: %w", err)
		}
		cfg.GuildID = id
	}

	if cfg.StateDir == "" {
		cfg.StateDir = "."
	}

	if cfg.NavidromeURL == "" {
		cfg.NavidromeURL = "http://127.0.0.1:4533"
	}

	return cfg, nil
}
