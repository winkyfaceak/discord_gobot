package main

import (
	"log"

	"discord_gobot/internal/bot"
	"discord_gobot/internal/commands"
	"discord_gobot/internal/config"
)

func main() {
	// Load configuration from environment variables (see config.Load)
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	// commands.All() returns every slash command we want to register
	// This keeps main.go small and prevents the main file from knowing
	// the details of every command
	b, err := bot.New(cfg, commands.All(cfg))
	if err != nil {
		log.Fatal(err)
	}

	// Run() connects to Discord, registers slash commands,
	// and keeps the process alive until CTRL+C
	if err := b.Run(); err != nil {
		log.Fatal(err)
	}
}
