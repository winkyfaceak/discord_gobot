package config

import (
	"errors"
	"os"
	"strings"
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
	// If empty, slash commands are registered globally
	GuildID string

	// AppID is optional.
	//
	// This is your Discord application/client ID
	// If empty, the bot will try to discover it from the logged-in bot user
	AppID string
}

// Load reads configuration from environment variables
//
// In fish, set them like this:
//
//	set -Ux DISCORD_TOKEN "your_raw_bot_token_here"
//	set -Ux GUILD_ID "your_test_server_id_here"
//
// GUILD_ID and APP_ID are optional
func Load() (Config, error) {
	cfg := Config{
		Token:   strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		GuildID: strings.TrimSpace(os.Getenv("GUILD_ID")),
		AppID:   strings.TrimSpace(os.Getenv("APP_ID")),
	}

	// The bot cannot run without a token, so fail early with a clear message
	if cfg.Token == "" {
		return Config{}, errors.New("DISCORD_TOKEN is not set")
	}

	return cfg, nil
}
