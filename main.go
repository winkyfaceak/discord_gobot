package main

import (
	"log"

	"discord_gobot/internal/bot"
	"discord_gobot/internal/commands"
	"discord_gobot/internal/config"
)

func main() {
	// Load configuration from environment variables
	//
	// Required:
	//   DISCORD_TOKEN
	//
	// Optional:
	//   GUILD_ID
	//   APP_ID
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// Create the bot
	//
	// commands.All() returns every slash command we want to register
	// This keeps main.go small and prevents the main file from knowing
	// the details of every command
	b, err := bot.New(cfg, commands.All())
	if err != nil {
		log.Fatal(err)
	}

	// Start the bot
	//
	// Run() opens the Discord websocket connection, registers slash commands,
	// and keeps the process alive until CTRL+C
	if err := b.Run(); err != nil {
		log.Fatal(err)
	}
}
